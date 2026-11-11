package providers

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
)

const (
	oracleStageOperation  = "stage-pending-vault-version"
	oracleDeleteOperation = "schedule-deprecated-vault-version-deletion"
)

type OracleStage struct {
	MutationID   string
	Target       provideridentity.OracleVaultVersion
	ETag         string
	Writer       providerlifecycle.Client
	SourceHandle string
	Load         func(context.Context, string) ([]byte, error)
	Authorize    rotation.AuthorizePreparation
}

type OracleRetirement struct {
	MutationID       string
	Target           provideridentity.OracleVaultVersion
	ETag             string
	DeleteAt         time.Time
	Revoker          providerlifecycle.Client
	Activation       rotation.ActivationEvidence
	Consumers        []rotation.ConsumerEvidence
	VerifyActivation rotation.VerifyActivation
	VerifyConsumer   rotation.VerifyConsumer
	Authorize        rotation.AuthorizeRevocation
}

// OracleRotation is populated by the trusted host registry for one Vault
// Secret. Its two operations share the Gateway's journal and authority path.
type OracleRotation struct {
	Plan       rotation.PreparationPlan
	Stage      *OracleStage
	Retirement *OracleRetirement
}

func (r OracleRotation) ResolveRotation(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if req.Provider != "oracle" || r.Plan.Old.Provider != "oracle" || req.PlanDigest != r.Plan.ID {
		return nil, effectgateway.ErrAdmissionRejected
	}
	if r.Stage != nil && r.Retirement != nil {
		stage, old := r.Stage.Target, r.Retirement.Target
		if stage.Region != old.Region || stage.TenancyID != old.TenancyID || stage.CompartmentID != old.CompartmentID ||
			stage.VaultID != old.VaultID || stage.SecretID != old.SecretID || stage.Version != old.Version {
			return nil, effectgateway.ErrAdmissionRejected
		}
	}
	switch req.Action {
	case hostbridge.ActionStage:
		return r.stage(ctx, req)
	case hostbridge.ActionRevoke:
		return r.retire(ctx, req)
	default:
		return nil, effectgateway.ErrAdmissionRejected
	}
}

// OraclePendingVersionName is the non-secret operation correlation marker sent
// to OCI. The signed request and independent reads use the same marker.
func OraclePendingVersionName(mutationID, planDigest string) string {
	return "kc-" + hostbridge.OperationDigest(mutationID, planDigest)[:48]
}

func (r OracleRotation) stage(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if r.Stage == nil {
		return nil, effectgateway.ErrAdmissionRejected
	}
	stage := *r.Stage
	t := stage.Target
	name := OraclePendingVersionName(req.MutationID, r.Plan.ID)
	if t.PlanTarget != r.Plan.Old.Target || t.Version == 0 || stage.ETag == "" ||
		stage.MutationID != req.MutationID || stage.SourceHandle == "" || stage.Load == nil ||
		req.TargetDigest != hostbridge.TargetDigest("oracle", oracleStageOperation, t.StageTarget(stage.ETag, name)) {
		return nil, effectgateway.ErrAdmissionRejected
	}
	return rotation.NewPrepareActionWithFence(ctx, req.MutationID, r.Plan, stage.Authorize,
		func(ctx context.Context, _ rotation.PreparationPlan, fence func(context.Context) error) (journal.Observation, error) {
			before, err := t.PreflightPending(ctx, stage.ETag, name)
			if err != nil {
				return journal.Observation{}, rotation.ErrActionNotCurrent
			}
			value, err := stage.Load(ctx, stage.SourceHandle)
			defer clear(value)
			if err != nil {
				return journal.Observation{}, rotation.ErrActionNotCurrent
			}
			_, err = stage.Writer.StageOraclePendingVaultVersionWithFence(ctx,
				oracleOperation(oracleStageOperation, t.Region+"/"+t.SecretID), t.Region, t.SecretID, stage.ETag, name, value, fence)
			if err != nil {
				return journal.Observation{}, oracleEffectError(err)
			}
			version, proof, err := t.VerifyPending(ctx, before)
			if err != nil || version == 0 || !journal.ValidDigest(proof) {
				return journal.Observation{}, journal.ErrUnknownEffect
			}
			return journal.Observation{State: journal.Acknowledged, ProofDigest: hostbridge.OperationDigest(req.MutationID, proof)}, nil
		})
}

func (r OracleRotation) retire(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if r.Retirement == nil {
		return nil, effectgateway.ErrAdmissionRejected
	}
	retirement := *r.Retirement
	t := retirement.Target
	deleteAt := retirement.DeleteAt.UTC().Truncate(time.Second)
	handle := strconv.FormatUint(t.Version, 10)
	if t.PlanTarget != r.Plan.Old.Target || t.Version == 0 || retirement.ETag == "" || deleteAt.IsZero() ||
		retirement.MutationID != req.MutationID ||
		req.TargetDigest != hostbridge.TargetDigest("oracle", oracleDeleteOperation, t.DeletionTarget(retirement.ETag, deleteAt)) {
		return nil, effectgateway.ErrAdmissionRejected
	}
	return rotation.NewFinalizeActionWithFence(ctx, req.MutationID, r.Plan, retirement.Activation, retirement.Consumers, time.Now(),
		retirement.VerifyActivation, retirement.VerifyConsumer, retirement.Authorize,
		func(ctx context.Context, plan rotation.PreparationPlan, old string) (rotation.VerifiedOldCredential, error) {
			return t.VerifyWithETag(ctx, plan, old, retirement.ETag)
		},
		func(ctx context.Context, _ rotation.Generation, old string, fence func(context.Context) error) (journal.Observation, error) {
			if old != handle {
				return journal.Observation{}, rotation.ErrActionNotCurrent
			}
			ack, err := retirement.Revoker.ScheduleOracleOldVersionDeletionWithFence(ctx,
				oracleOperation(oracleDeleteOperation, t.Region+"/"+t.SecretID+"/version/"+handle), t.Region, t.SecretID, retirement.ETag, t.Version, deleteAt, fence)
			if err != nil {
				return journal.Observation{}, oracleEffectError(err)
			}
			// The provider ACK confirms only scheduling. Permanent deletion
			// requires a separate observation after the scheduled time.
			return journal.Observation{State: journal.Acknowledged, ProofDigest: hostbridge.OperationDigest(req.TargetDigest, ack.ProofDigest)}, nil
		})
}

func oracleOperation(operation, target string) providerlifecycle.HostAuthorize {
	return func(_ context.Context, provider, op, exact string) error {
		if provider != "oracle" || op != operation || exact != target {
			return effectgateway.ErrAdmissionRejected
		}
		return nil
	}
}

func oracleEffectError(err error) error {
	if errors.Is(err, providerlifecycle.ErrOracleEffectUnknown) {
		return journal.ErrUnknownEffect
	}
	return rotation.ErrActionNotCurrent
}
