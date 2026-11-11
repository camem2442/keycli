package effectgateway

import (
	"context"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
)

// CloudflareRevocationRecovery corroborates a still-active old credential using an
// independently authenticated Cloudflare account and token GET requests.
// Its fields are supplied by the host from the original revocation plan.
type CloudflareRevocationRecovery struct {
	Binding         authoritybinding.Binding
	Plan            rotation.PreparationPlan
	CanonicalTarget string
	Token           provideridentity.CloudflareAccountToken
}

func (r CloudflareRevocationRecovery) ObserveRecovery(ctx context.Context, binding authoritybinding.Binding) (RecoveryEvidence, error) {
	if binding != r.Binding || binding.Provider != "cloudflare" || r.Plan.ID != binding.PlanDigest ||
		r.CanonicalTarget == "" || binding.TargetDigest != hostbridge.TargetDigest("cloudflare", "delete-exact-account-token", r.CanonicalTarget) {
		return RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	identity, err := r.Token.Verify(ctx, r.Plan, r.Token.TokenID)
	if err != nil || identity.Provider != binding.Provider || identity.PlanDigest != binding.PlanDigest ||
		identity.PlanTarget != r.Plan.Old.Target || identity.CredentialID != r.Token.TokenID ||
		identity.Generation != r.Plan.Old.Number || identity.ObservedAt.IsZero() ||
		identity.ObservedAt.After(time.Now()) || !journal.ValidDigest(identity.ProofDigest) {
		return RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	return RecoveryEvidence{
		State: journal.ObservedNoEffect, ObservedAt: identity.ObservedAt,
		ProviderIdentityDigest: rotation.RevocationIdentityDigest(identity), SourceDigest: identity.ProofDigest,
	}, nil
}
