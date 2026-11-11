package effectgateway

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
)

var errDuplicateReservation = errors.New("Authority already recorded the bound reservation")

// RegisteredOperation is a trusted projection of a protected registration.
// The registration resolves canonical target and independently verified digests.
// The host identity binds the registered operation and exact mutation separately.
type RegisteredOperation struct {
	Identity          hostbridge.Identity
	Provider          string
	ProviderOperation string
	CanonicalTarget   string
	ResourceDigest    string
	PlanDigest        string
	GrantDigest       string
}

type OperationRegistry interface {
	Resolve(ctx context.Context, goalID, mutationID string) (RegisteredOperation, error)
}

// LocalAuthority binds each submission independently. It does not accept a
// socket path, raw provider target, or registry contents from the writer.
type LocalAuthority struct {
	Registry OperationRegistry
	Admitter *hostbridge.LocalAdmitter
	Now      func() time.Time

	// Execution is supplied explicitly by the trusted host. Without it, the
	// existing v1 execution preview remains advisory.
	Execution *hostbridge.GuardedExecution
}

func (a LocalAuthority) Bind(ctx context.Context, req Request) (BoundAuthority, error) {
	if a.Registry == nil || a.Admitter == nil {
		return nil, hostbridge.ErrLocalAuthorityUnavailable
	}
	reg, err := a.Registry.Resolve(ctx, req.GoalID, req.MutationID)
	if err != nil {
		return nil, ErrAdmissionRejected
	}
	id := reg.Identity
	action, mutating, err := hostbridge.ClassifyOperation(reg.Provider, reg.ProviderOperation)
	if err != nil || !mutating || req.GoalID != id.GoalID || req.MutationID != id.MutationID ||
		req.Generation != id.WriterGeneration || req.Owner != id.WriterOwner ||
		req.Session != id.WriterSession || req.Provider != reg.Provider || req.Action != action ||
		reg.CanonicalTarget == "" || strings.ContainsRune(reg.CanonicalTarget, 0) {
		return nil, hostbridge.ErrBindingMismatch
	}
	binding := reg.authorityBinding()
	if !binding.Valid() || binding.TargetDigest != req.TargetDigest || reg.PlanDigest != req.PlanDigest || reg.GrantDigest != req.GrantDigest {
		return nil, hostbridge.ErrBindingMismatch
	}
	now := a.Now
	if now == nil {
		now = time.Now
	}
	return &localBoundAuthority{
		admitter:      a.Admitter,
		execution:     a.Execution,
		now:           now,
		binding:       binding,
		requestExpiry: req.ExpiresAt,
	}, nil
}

func (reg RegisteredOperation) authorityBinding() hostbridge.AuthorityBinding {
	id := reg.Identity
	return hostbridge.AuthorityBinding{
		GoalID: id.GoalID, MutationID: id.MutationID, WriterGeneration: id.WriterGeneration,
		Owner: id.WriterOwner, Session: id.WriterSession, Provider: reg.Provider,
		Operation: id.OperationID, ResourceDigest: reg.ResourceDigest,
		TargetDigest: hostbridge.TargetDigest(reg.Provider, reg.ProviderOperation, reg.CanonicalTarget),
		PlanDigest:   reg.PlanDigest, GrantDigest: reg.GrantDigest,
	}
}

type localBoundAuthority struct {
	admitter      *hostbridge.LocalAdmitter
	execution     *hostbridge.GuardedExecution
	now           func() time.Time
	binding       hostbridge.AuthorityBinding
	requestExpiry time.Time
	expiry        time.Time
}

func (a *localBoundAuthority) Preflight(ctx context.Context) error {
	expiry, err := a.admitter.Preflight(ctx, a.binding, a.now)
	if err != nil {
		return err
	}
	a.expiry = expiry
	if a.execution == nil {
		return nil
	}
	remaining := a.executionExpiry().Sub(a.now())
	if remaining <= 0 {
		return hostbridge.ErrAdmissionInvalid
	}
	reserveCtx, cancel := context.WithTimeout(ctx, remaining)
	defer cancel()
	reservation, err := a.admitter.Reserve(reserveCtx, a.binding)
	if err != nil {
		return err
	}
	if reservation.Reason == "DUPLICATE" {
		return errDuplicateReservation
	}
	if reservation.LeaseExpiresAt.Before(a.expiry) {
		a.expiry = reservation.LeaseExpiresAt
	}
	if !a.executionCurrent() {
		return hostbridge.ErrAdmissionInvalid
	}
	return nil
}

func (a *localBoundAuthority) VerifyExecution(ctx context.Context) error {
	if !a.now().Before(a.expiry) {
		return hostbridge.ErrAdmissionInvalid
	}
	return a.admitter.VerifyExecution(ctx, a.binding)
}

func (a *localBoundAuthority) verifyAtDispatch(ctx context.Context, guard journal.DispatchGuard) error {
	if a.execution == nil {
		return a.VerifyExecution(ctx)
	}
	expiry := a.executionExpiry()
	return guard.WithLockFile(func(lock *os.File) error {
		return a.execution.Verify(ctx, a.binding, expiry, a.now, lock)
	})
}

func (a *localBoundAuthority) executionExpiry() time.Time {
	if a.requestExpiry.Before(a.expiry) {
		return a.requestExpiry
	}
	return a.expiry
}

func (a *localBoundAuthority) executionCurrent() bool {
	return a.execution == nil || a.now().Before(a.executionExpiry())
}
