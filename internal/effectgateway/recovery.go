package effectgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
)

// RecoveryGrant is the trusted read authority for a specific persisted
// mutation and immutable provider identity. Its digest identifies a separate
// read grant, not the grant originally used to dispatch the mutation.
type RecoveryGrant struct {
	Binding                authoritybinding.Binding
	GrantDigest            string
	ProviderIdentityDigest string
	ExpiresAt              time.Time
}

type RecoveryAuthorizer interface {
	AuthorizeRecovery(context.Context, authoritybinding.Binding) (RecoveryGrant, error)
}

type RecoveryEvidence struct {
	State                  journal.State
	ObservedAt             time.Time
	ProviderIdentityDigest string
	SourceDigest           string
}

// RecoveryObserver performs a read-only provider observation. SourceDigest
// covers the corresponding evidence metadata, without credential values.
type RecoveryObserver interface {
	ObserveRecovery(context.Context, authoritybinding.Binding) (RecoveryEvidence, error)
}

// Reconcile accepts an operation ID from the trusted host. The journal owns
// the original binding and retains its writer lock through observation and
// settlement. The caller has no way to propose a resulting state or digest.
func (g *Gateway) Reconcile(ctx context.Context, operationID string) (journal.Record, error) {
	if g == nil || g.Journal.Root == "" || g.RecoveryAuthority == nil || g.RecoveryObserver == nil {
		return journal.Record{}, ErrNotConfigured
	}
	return g.Journal.ReconcileBound(operationID, func(record journal.Record) (journal.Observation, error) {
		binding := *record.Binding
		grant, err := g.RecoveryAuthority.AuthorizeRecovery(ctx, binding)
		started := g.now()
		if err != nil || ctx.Err() != nil || grant.Binding != binding ||
			grant.GrantDigest == binding.GrantDigest || !journal.ValidDigest(grant.GrantDigest) ||
			!journal.ValidDigest(grant.ProviderIdentityDigest) || !started.Before(grant.ExpiresAt) {
			return journal.Observation{}, ErrAdmissionRejected
		}
		readCtx, cancel := context.WithTimeout(ctx, grant.ExpiresAt.Sub(started))
		defer cancel()
		evidence, err := g.RecoveryObserver.ObserveRecovery(readCtx, binding)
		finished := g.now()
		if err != nil || readCtx.Err() != nil || !finished.Before(grant.ExpiresAt) ||
			evidence.ObservedAt.Before(started) || evidence.ObservedAt.After(finished) ||
			evidence.ProviderIdentityDigest != grant.ProviderIdentityDigest ||
			!journal.ValidDigest(evidence.SourceDigest) ||
			(evidence.State != journal.ObservedApplied && evidence.State != journal.ObservedNoEffect) {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		evidence.ObservedAt = evidence.ObservedAt.UTC()
		raw, err := json.Marshal(struct {
			Binding     authoritybinding.Binding
			GrantDigest string
			Evidence    RecoveryEvidence
		}{binding, grant.GrantDigest, evidence})
		if err != nil {
			return journal.Observation{}, err
		}
		digest := sha256.Sum256(raw)
		return journal.Observation{State: evidence.State, ProofDigest: hex.EncodeToString(digest[:])}, nil
	})
}
