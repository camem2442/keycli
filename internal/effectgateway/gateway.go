package effectgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
)

// Gateway is the single effect boundary. Every field is a dependency the host
// supplies; the gateway owns no storage of its own.
type Gateway struct {
	Claims    ClaimAuthority
	Keys      KeyRing
	Authority Authority
	Journal   journal.Store
	Provider  Provider
	Now       func() time.Time

	RotationActions   RotationResolver
	RecoveryAuthority RecoveryAuthorizer
	RecoveryObserver  RecoveryObserver
}

func (g *Gateway) now() time.Time {
	if g != nil && g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *Gateway) configured() bool {
	return g != nil && g.Claims != nil && g.Keys != nil && g.Authority != nil &&
		(g.Provider != nil || g.RotationActions != nil) && g.Journal.Root != ""
}

// Submit authenticates, authorises and executes exactly one intent.
//
// Order matters: signature, expiry, claim snapshot, host admission, durable
// journal intent, effect-boundary re-check, provider call. An unconfirmed
// Authority handoff or provider outcome latches UNKNOWN_EFFECT for reconciliation.
func (g *Gateway) Submit(ctx context.Context, env Envelope) (Receipt, error) {
	if !g.configured() {
		return Receipt{}, ErrNotConfigured
	}
	if ctx == nil || ctx.Err() != nil {
		return Receipt{Status: StatusNoEffect}, ErrAuthorityUnavail
	}
	now := g.now()
	req := env.Request
	if err := req.validate(now); err != nil {
		return Receipt{}, err
	}

	key, err := g.Keys.Key(ctx, req.GoalID, req.Owner, req.Session, req.Generation)
	if err != nil || !verifySignature(env, key) {
		return Receipt{}, ErrUnauthenticated
	}

	claim, err := g.Claims.Snapshot(ctx, req.GoalID)
	if err != nil {
		return Receipt{}, ErrAuthorityUnavail
	}
	if err := checkClaim(claim, req, now); err != nil {
		return Receipt{}, err
	}

	bound, err := g.Authority.Bind(ctx, req)
	if err != nil || bound == nil {
		return Receipt{Status: StatusNoEffect}, authorityError(err)
	}
	var action RotationAction
	if g.RotationActions != nil {
		action, err = g.RotationActions.ResolveRotation(ctx, req)
		if err != nil || !matchesRotation(action, req) {
			return Receipt{Status: StatusNoEffect}, ErrAdmissionRejected
		}
	}
	if err := bound.Preflight(ctx); err != nil {
		if errors.Is(err, errDuplicateReservation) {
			return Receipt{MutationID: req.MutationID, Status: StatusDuplicate,
				Note: "mutation already reserved by Authority; no provider call"}, nil
		}
		return Receipt{Status: StatusNoEffect}, authorityError(err)
	}

	calls := 0
	noEffectCode := ""
	intent := journal.Intent{
		OperationID:  req.MutationID,
		TargetDigest: req.TargetDigest,
		PlanDigest:   req.PlanDigest,
		GrantDigest:  req.GrantDigest,
	}
	effect := func(verifyExecution func(context.Context) error, executionCurrent func() bool) (journal.Observation, error) {
		// The journal is durable and locked. Recheck the writer and the exact
		// authority binding before allowing any provider call.
		noEffect := func(reason string) (journal.Observation, error) {
			noEffectCode = reason
			return journal.Observation{State: journal.ObservedNoEffect,
				ProofDigest: localNoEffectProof(req.MutationID, reason)}, nil
		}
		checkFence := func(ctx context.Context) error {
			if ctx.Err() != nil || !g.now().Before(req.ExpiresAt) {
				return fenceRejection("REQUEST_EXPIRED")
			}
			fresh, snapErr := g.Claims.Snapshot(ctx, req.GoalID)
			if snapErr != nil {
				return fenceRejection("AUTHORITY_UNAVAILABLE")
			}
			if checkClaim(fresh, req, g.now()) != nil {
				return fenceRejection("STALE_CLAIM")
			}
			checkKey := func() bool {
				key, err := g.Keys.Key(ctx, req.GoalID, req.Owner, req.Session, req.Generation)
				return err == nil && verifySignature(env, key)
			}
			if !checkKey() {
				return fenceRejection("SESSION_KEY_INVALID")
			}
			if err := verifyExecution(ctx); err != nil {
				switch {
				case errors.Is(err, hostbridge.ErrGuardedExecutionUnknown):
					return err
				case errors.Is(err, hostbridge.ErrExecutionFenceUnavailable):
					return fenceRejection("EXECUTION_FENCE_UNAVAILABLE")
				case errors.Is(err, hostbridge.ErrLocalAuthorityUnavailable):
					return fenceRejection("AUTHORITY_UNAVAILABLE")
				default:
					return fenceRejection("EXECUTION_FENCE_REJECTED")
				}
			}
			// Verification can block on IPC. Re-read the claim and signing key
			// and recheck expiry before handing off to the provider.
			fresh, snapErr = g.Claims.Snapshot(ctx, req.GoalID)
			if snapErr != nil {
				return fenceRejection("AUTHORITY_UNAVAILABLE")
			}
			if !checkKey() {
				return fenceRejection("SESSION_KEY_INVALID")
			}
			if ctx.Err() != nil || !g.now().Before(req.ExpiresAt) {
				return fenceRejection("REQUEST_EXPIRED")
			}
			if checkClaim(fresh, req, g.now()) != nil {
				return fenceRejection("STALE_CLAIM")
			}
			if !executionCurrent() {
				return fenceRejection("EXECUTION_FENCE_EXPIRED")
			}
			return nil
		}
		outcome := func(err error) (journal.Observation, error) {
			var rejection fenceRejection
			switch {
			case errors.As(err, &rejection):
				return noEffect(string(rejection))
			case errors.Is(err, rotation.ErrActionNotCurrent):
				return noEffect("ROTATION_ACTION_NOT_CURRENT")
			default:
				return journal.Observation{}, err
			}
		}
		if action != nil {
			if _, finalizing := action.(rotation.FinalizeAction); finalizing {
				if err := rotation.CheckRollbackFence(g.Journal, req.PlanDigest); err != nil {
					return outcome(err)
				}
			}
			fencePassed := false
			observation, err := action.Execute(ctx, func(actionCtx context.Context) error {
				err := checkFence(actionCtx)
				fencePassed = err == nil
				return err
			})
			if fencePassed && !errors.Is(err, rotation.ErrActionNotCurrent) {
				calls++
			}
			if err != nil {
				return outcome(err)
			}
			return observation, nil
		}
		if err := checkFence(ctx); err != nil {
			return outcome(err)
		}
		calls++
		result, applyErr := g.Provider.Apply(ctx, req)
		if applyErr != nil {
			return journal.Observation{}, errors.New("provider outcome unavailable")
		}
		if result.ProofDigest == "" || !journal.ValidDigest(result.ProofDigest) {
			return journal.Observation{}, errors.New("provider result has no usable proof digest")
		}
		switch result.State {
		case StateAcknowledged:
			return journal.Observation{State: journal.Acknowledged, ProofDigest: result.ProofDigest}, nil
		case StateObservedNoEffect:
			return journal.Observation{State: journal.ObservedNoEffect, ProofDigest: result.ProofDigest}, nil
		default:
			return journal.Observation{}, errors.New("provider result state is not recognised")
		}
	}
	var record journal.Record
	local, _ := bound.(*localBoundAuthority)
	if local != nil {
		// Use the protected registration projection that was sent to Authority.
		record, err = g.Journal.DispatchBound(intent, local.binding, func(guard journal.DispatchGuard) (journal.Observation, error) {
			return effect(func(ctx context.Context) error { return local.verifyAtDispatch(ctx, guard) }, local.executionCurrent)
		})
	} else {
		record, err = g.Journal.Dispatch(intent, func() (journal.Observation, error) {
			return effect(bound.VerifyExecution, func() bool { return true })
		})
	}

	switch {
	case errors.Is(err, journal.ErrDuplicate):
		return Receipt{MutationID: req.MutationID, Status: StatusDuplicate, ProviderCalls: 0,
			Note: "mutation already recorded; no provider call"}, nil
	case errors.Is(err, journal.ErrIntentMismatch):
		return Receipt{}, ErrMutationConflict
	case errors.Is(err, journal.ErrTargetPending):
		return Receipt{}, ErrTargetPending
	case errors.Is(err, journal.ErrBusy):
		return Receipt{}, ErrGatewayBusy
	case errors.Is(err, journal.ErrUnknownEffect):
		return Receipt{MutationID: req.MutationID, Status: StatusUnknown, ProviderCalls: calls,
			OperationState: string(record.State), ProofDigest: record.ProofDigest,
			Note: "unconfirmed dispatch outcome; reconcile before any retry"}, nil
	case err != nil:
		return Receipt{}, ErrJournalUnavailable
	}

	status := StatusNoEffect
	note := noEffectCode
	if record.State == journal.Acknowledged {
		status = StatusApplied
		if action != nil && req.Provider == "oracle" && req.Action == hostbridge.ActionRevoke {
			note = "OCI Vault acknowledged the deletion schedule; permanent deletion requires separate observation"
		}
	}
	var settlement string
	if local != nil && local.execution != nil {
		settlement = local.finish(ctx, record.State)
	}
	return Receipt{MutationID: req.MutationID, Status: status, ProviderCalls: calls,
		OperationState: string(record.State), ProofDigest: record.ProofDigest,
		AuthoritySettlement: settlement, Note: note}, nil
}

func authorityError(err error) error {
	if errors.Is(err, hostbridge.ErrReservationUnknown) {
		return ErrReservationUnknown
	}
	if errors.Is(err, hostbridge.ErrLocalAuthorityUnavailable) || errors.Is(err, hostbridge.ErrExecutionFenceUnavailable) {
		return ErrAuthorityUnavail
	}
	return ErrAdmissionRejected
}

func checkClaim(claim ClaimSnapshot, req Request, now time.Time) error {
	if claim.GoalID != req.GoalID {
		return ErrAuthorityUnavail
	}
	if claim.Generation != req.Generation {
		return ErrStaleGeneration
	}
	if claim.Session != req.Session {
		return ErrSessionMismatch
	}
	if claim.Owner != req.Owner {
		return ErrOwnerMismatch
	}
	if claim.State != "ACTIVE" {
		return ErrClaimNotActive
	}
	if !claim.LeaseExpiresAt.IsZero() && !now.Before(claim.LeaseExpiresAt) {
		return ErrClaimLeaseExpired
	}
	return nil
}

// localNoEffectProof records a gateway-local fact ("no provider call was made").
// It is not a provider observation and must never be read as one.
func localNoEffectProof(mutationID, reason string) string {
	sum := sha256.Sum256([]byte("effectgateway|no-effect|" + mutationID + "|" + reason))
	return hex.EncodeToString(sum[:])
}
