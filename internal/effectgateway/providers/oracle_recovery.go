package providers

import (
	"context"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
)

// OraclePendingStageRecovery reads the exact operation marker under the
// original Current baseline without reusing its write credential.
type OraclePendingStageRecovery struct {
	Binding authoritybinding.Binding
	Target  provideridentity.OracleVaultVersion
	ETag    string
}

func (r OraclePendingStageRecovery) IdentityDigest() string {
	name := OraclePendingVersionName(r.Binding.MutationID, r.Binding.PlanDigest)
	return hostbridge.TargetDigest("oracle", oracleStageOperation, r.Target.StageTarget(r.ETag, name))
}

func (r OraclePendingStageRecovery) ObserveRecovery(ctx context.Context, binding authoritybinding.Binding) (effectgateway.RecoveryEvidence, error) {
	identity := r.IdentityDigest()
	if binding != r.Binding || binding.Provider != "oracle" || binding.TargetDigest != identity || r.ETag == "" {
		return effectgateway.RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	name := OraclePendingVersionName(binding.MutationID, binding.PlanDigest)
	version, proof, err := r.Target.RecoverPending(ctx, name)
	if err != nil || version == 0 || !journal.ValidDigest(proof) {
		return effectgateway.RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	return effectgateway.RecoveryEvidence{
		State: journal.ObservedApplied, ObservedAt: time.Now().UTC(), ProviderIdentityDigest: identity,
		SourceDigest: hostbridge.OperationDigest(binding.MutationID, proof),
	}, nil
}
