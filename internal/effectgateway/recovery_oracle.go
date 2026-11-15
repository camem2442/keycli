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
	// ETag is mandatory for the newer pinned registry target. Legacy records
	// keep the earlier target format to preserve read-only recovery.
	ETag string
}

func (r OracleDeletionScheduleRecovery) ObserveRecovery(ctx context.Context, binding authoritybinding.Binding) (RecoveryEvidence, error) {
	version := strconv.FormatUint(r.Version.Version, 10)
	target := r.Version.Region + "/" + r.Version.SecretID + "/version/" + version
	deleteAt := r.DeleteAt
	if r.ETag != "" {
		deleteAt = deleteAt.UTC().Truncate(time.Second)
		target = r.Version.DeletionTarget(r.ETag, deleteAt)
	}
	if binding != r.Binding || binding.Provider != "oracle" || r.Plan.ID != binding.PlanDigest || r.DeleteAt.IsZero() ||
		binding.TargetDigest != hostbridge.TargetDigest("oracle", "schedule-deprecated-vault-version-deletion", target) {
		return RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	var identity rotation.VerifiedOldCredential
	var err error
	if r.ETag == "" {
		identity, err = r.Version.VerifyScheduledDeletion(ctx, r.Plan, version, deleteAt)
	} else {
		identity, err = r.Version.VerifyScheduledDeletionFromETag(ctx, r.Plan, version, deleteAt, r.ETag)
	}
	if err != nil {
		return RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	proof := sha256.Sum256([]byte(identity.ProofDigest + "\x00" + deleteAt.UTC().Format(time.RFC3339Nano)))
	return RecoveryEvidence{
		State: journal.ObservedApplied, ObservedAt: identity.ObservedAt,
		ProviderIdentityDigest: rotation.RevocationIdentityDigest(identity), SourceDigest: hex.EncodeToString(proof[:]),
	}, nil
}
