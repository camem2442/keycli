package effectgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
)

// OracleDeletionScheduleRecovery corroborates the exact scheduling effect.
// The host loads the deletion time and metadata from its original plan.
type OracleDeletionScheduleRecovery struct {
	Binding  authoritybinding.Binding
	Plan     rotation.PreparationPlan
	Version  provideridentity.OracleVaultVersion
	DeleteAt time.Time
}

func (r OracleDeletionScheduleRecovery) ObserveRecovery(ctx context.Context, binding authoritybinding.Binding) (RecoveryEvidence, error) {
	version := strconv.FormatUint(r.Version.Version, 10)
	target := r.Version.Region + "/" + r.Version.SecretID + "/version/" + version
	if binding != r.Binding || binding.Provider != "oracle" || r.Plan.ID != binding.PlanDigest || r.DeleteAt.IsZero() ||
		binding.TargetDigest != hostbridge.TargetDigest("oracle", "schedule-deprecated-vault-version-deletion", target) {
		return RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	identity, err := r.Version.VerifyScheduledDeletion(ctx, r.Plan, version, r.DeleteAt)
	if err != nil {
		return RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	proof := sha256.Sum256([]byte(identity.ProofDigest + "\x00" + r.DeleteAt.UTC().Format(time.RFC3339Nano)))
	return RecoveryEvidence{
		State: journal.ObservedApplied, ObservedAt: identity.ObservedAt,
		ProviderIdentityDigest: rotation.RevocationIdentityDigest(identity), SourceDigest: hex.EncodeToString(proof[:]),
	}, nil
}
