package providers

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/delivery/cloudflaredelivery"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
	"github.com/camem2442/keycli/internal/target/cloudflare"
)

const (
	cloudflareIssueOperation  = "create-account-token"
	cloudflareDeleteOperation = "delete-exact-account-token"
	cloudflareStageOperation  = "stage-inactive-worker-version-secret"
)

// CloudflareTokenIssue uses an issuer client independently enrolled from the
// revoker. The host supplies the allowlist of runtime-only permission groups.
type CloudflareTokenIssue struct {
	MutationID        string
	Name              string
	PermissionGroupID string
	ExpiresAt         time.Time
	Issuer            providerlifecycle.Client
	IssuerPermissions provideridentity.Reader
	PermissionReader  provideridentity.Reader
	Sink              providerlifecycle.SecretSink
	Authorize         rotation.AuthorizePreparation
}

type CloudflareWorkerStage struct {
	MutationID           string
	Request              cloudflare.VersionStageRequest
	WranglerSHA256       string
	SourceHandle         string
	Load                 func(context.Context, string) ([]byte, error)
	Observer             cloudflaredelivery.Observer
	ExpectedActiveDigest string
	Authorize            rotation.AuthorizePreparation
}

type CloudflareRetirement struct {
	MutationID       string
	Target           provideridentity.CloudflareAccountToken
	Revoker          providerlifecycle.Client
	Activation       rotation.ActivationEvidence
	Consumers        []rotation.ConsumerEvidence
	VerifyActivation rotation.VerifyActivation
	VerifyConsumer   rotation.VerifyConsumer
	Authorize        rotation.AuthorizeRevocation
}

// CloudflareRotation binds effects to one host-configured account and plan.
type CloudflareRotation struct {
	Plan                    rotation.PreparationPlan
	AccountID               string
	RuntimePermissionGroups []string
	Issue                   *CloudflareTokenIssue
	WorkerStage             *CloudflareWorkerStage
	Retirement              *CloudflareRetirement
}

func (r CloudflareRotation) ResolveRotation(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if req.Provider != "cloudflare" || r.Plan.Old.Provider != "cloudflare" || req.PlanDigest != r.Plan.ID || !cloudflareID(r.AccountID) {
		return nil, effectgateway.ErrAdmissionRejected
	}
	switch req.Action {
	case hostbridge.ActionIssue:
		return r.issue(ctx, req)
	case hostbridge.ActionStage:
		return r.stage(ctx, req)
	case hostbridge.ActionRevoke:
		return r.retire(ctx, req)
	default:
		return nil, effectgateway.ErrAdmissionRejected
	}
}

func (r CloudflareRotation) issue(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if r.Issue == nil {
		return nil, effectgateway.ErrAdmissionRejected
	}
	issue := *r.Issue
	canonical := r.AccountID + "/tokens/" + issue.Name + "/permission-group/" + issue.PermissionGroupID
	if issue.MutationID != req.MutationID || !cloudflareID(issue.PermissionGroupID) || issue.Sink == nil ||
		!slices.Contains(r.RuntimePermissionGroups, issue.PermissionGroupID) ||
		req.TargetDigest != hostbridge.TargetDigest("cloudflare", cloudflareIssueOperation, canonical) {
		return nil, effectgateway.ErrAdmissionRejected
	}
	return rotation.NewPrepareActionWithFence(ctx, req.MutationID, r.Plan, issue.Authorize,
		func(ctx context.Context, _ rotation.PreparationPlan, fence func(context.Context) error) (journal.Observation, error) {
			if err := issue.IssuerPermissions.VerifyWorkersRuntimeGroup(ctx, r.AccountID, issue.PermissionGroupID); err != nil {
				return journal.Observation{}, rotation.ErrActionNotCurrent
			}
			if _, found, err := issue.Sink.Lookup(ctx, req.MutationID); err != nil || found {
				return journal.Observation{}, journal.ErrUnknownEffect
			}
			entered := false
			result, err := issue.Issuer.IssueCloudflareAccountToken(ctx, func(ctx context.Context, provider, operation, target string) error {
				if provider != "cloudflare" || operation != cloudflareIssueOperation || target != canonical {
					return effectgateway.ErrAdmissionRejected
				}
				if err := fence(ctx); err != nil {
					return err
				}
				entered = true
				return nil
			}, r.AccountID, issue.Name, issue.PermissionGroupID, issue.ExpiresAt, req.MutationID, issue.Sink)
			if err != nil {
				if !entered {
					return journal.Observation{}, rotation.ErrActionNotCurrent
				}
				return journal.Observation{}, journal.ErrUnknownEffect
			}
			return journal.Observation{State: journal.Acknowledged, ProofDigest: result.ProofDigest}, nil
		})
}

func (r CloudflareRotation) stage(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if r.WorkerStage == nil {
		return nil, effectgateway.ErrAdmissionRejected
	}
	stage := *r.WorkerStage
	target := stage.Request
	target.Env = slices.Clone(target.Env)
	tag := "kc-" + req.MutationID
	canonical := strings.Join([]string{r.AccountID, target.Worker, target.Environment, target.SecretName}, "/")
	if stage.MutationID != req.MutationID || target.AccountID != r.AccountID ||
		stage.SourceHandle == "" || stage.Load == nil || stage.Observer == nil || stage.ExpectedActiveDigest == "" ||
		(target.VersionTag != "" && target.VersionTag != tag) ||
		req.TargetDigest != hostbridge.TargetDigest("cloudflare", cloudflareStageOperation, canonical) {
		return nil, effectgateway.ErrAdmissionRejected
	}
	target.VersionTag = tag
	return rotation.NewPrepareActionWithFence(ctx, req.MutationID, r.Plan, stage.Authorize,
		func(ctx context.Context, _ rotation.PreparationPlan, fence func(context.Context) error) (journal.Observation, error) {
			before, err := stage.Observer.Observe(ctx, target.AccountID, target.Worker)
			if err != nil || before.AccountID != target.AccountID || before.Worker != target.Worker ||
				cloudflaredelivery.ActiveDigest(before) != stage.ExpectedActiveDigest ||
				len(before.LatestVersionIDs) == 0 || len(before.LatestVersionIDs) >= 100 {
				return journal.Observation{}, rotation.ErrActionNotCurrent
			}
			result, err := cloudflare.StagePinnedVersionSecret(ctx, target, stage.WranglerSHA256,
				func(ctx context.Context) ([]byte, error) { return stage.Load(ctx, stage.SourceHandle) }, fence)
			if result.State == cloudflare.VersionNotAttempted {
				return journal.Observation{}, rotation.ErrActionNotCurrent
			}
			_, proof, observeErr := cloudflaredelivery.VerifyStageVersion(ctx, stage.Observer, before, target.SecretName, tag, req.MutationID, req.TargetDigest)
			if err != nil || result.State != cloudflare.VersionAcknowledgedUnverified || observeErr != nil {
				return journal.Observation{}, journal.ErrUnknownEffect
			}
			return journal.Observation{State: journal.Acknowledged, ProofDigest: proof}, nil
		})
}

func (r CloudflareRotation) retire(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if r.Retirement == nil {
		return nil, effectgateway.ErrAdmissionRejected
	}
	retirement := *r.Retirement
	target := retirement.Target
	canonical := r.AccountID + "/tokens/" + target.TokenID
	if target.AccountID != r.AccountID || target.PlanTarget != r.Plan.Old.Target || !cloudflareID(target.TokenID) ||
		retirement.MutationID != req.MutationID || req.TargetDigest != hostbridge.TargetDigest("cloudflare", cloudflareDeleteOperation, canonical) {
		return nil, effectgateway.ErrAdmissionRejected
	}
	return rotation.NewFinalizeAction(ctx, req.MutationID, r.Plan, retirement.Activation, retirement.Consumers, time.Now(),
		retirement.VerifyActivation, retirement.VerifyConsumer, retirement.Authorize, target.Verify,
		func(ctx context.Context, _ rotation.Generation, handle string) (journal.Observation, error) {
			if handle != target.TokenID {
				return journal.Observation{}, rotation.ErrActionNotCurrent
			}
			result, err := retirement.Revoker.DeleteCloudflareAccountToken(ctx, func(_ context.Context, provider, operation, exactTarget string) error {
				if provider != "cloudflare" || operation != cloudflareDeleteOperation || exactTarget != canonical {
					return effectgateway.ErrAdmissionRejected
				}
				return nil
			}, target.AccountID, handle)
			return journal.Observation{State: journal.Acknowledged, ProofDigest: result.ProofDigest}, err
		})
}

func cloudflareID(id string) bool {
	return len(id) == 32 && strings.Trim(id, "0123456789abcdef") == ""
}
