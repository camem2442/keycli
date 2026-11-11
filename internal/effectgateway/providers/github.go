package providers

import (
	"context"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
	"github.com/camem2442/keycli/internal/target/gh"
)

const (
	githubStageOperation  = "stage-repository-generation-secret"
	githubDeleteOperation = "delete-exact-repository-secret"
)

type GitHubStage struct {
	MutationID   string
	Target       gh.SecretRESTTarget
	Client       GitHubGenerationWriter
	SourceHandle string
	Load         func(context.Context, string) ([]byte, error)
	Authorize    rotation.AuthorizePreparation
}

// GitHubGenerationWriter is implemented by the encrypted GitHub REST client.
type GitHubGenerationWriter interface {
	CreateRepositoryGenerationFrom(context.Context, gh.SecretRESTTarget, func(context.Context) ([]byte, error), func(context.Context) error) (gh.SecretWriteOutcome, error)
}

type GitHubRetirement struct {
	MutationID       string
	Target           provideridentity.GitHubRepositorySecret
	Client           providerlifecycle.Client
	Activation       rotation.ActivationEvidence
	Consumers        []rotation.ConsumerEvidence
	VerifyActivation rotation.VerifyActivation
	VerifyConsumer   rotation.VerifyConsumer
	Authorize        rotation.AuthorizeRevocation
}

// GitHubRotation is configured by the trusted host with one repository and
// generation plan. The Gateway owns the journal and writer fence.
type GitHubRotation struct {
	Plan       rotation.PreparationPlan
	Stage      *GitHubStage
	Retirement *GitHubRetirement
}

func (r GitHubRotation) ResolveRotation(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if req.Provider != "github" || r.Plan.Old.Provider != "github" || req.PlanDigest != r.Plan.ID {
		return nil, effectgateway.ErrAdmissionRejected
	}
	if r.Stage != nil && r.Retirement != nil {
		next, old := r.Stage.Target, r.Retirement.Target
		if next.RepositoryID != old.RepositoryID || next.RepositoryOwnerID != old.OwnerID ||
			!strings.EqualFold(next.Repository, old.Repository) {
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

func (r GitHubRotation) stage(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if r.Stage == nil {
		return nil, effectgateway.ErrAdmissionRejected
	}
	stage := *r.Stage
	t := stage.Target
	target, err := providerlifecycle.GitHubGenerationTarget(t.Repository, t.Destination, t.RepositoryID, t.RepositoryOwnerID, r.Plan.New.Number)
	if err != nil || t.Scope != "repository" || !strings.EqualFold(r.Plan.Old.Target, t.Repository) ||
		stage.Client == nil || stage.Load == nil || stage.SourceHandle == "" ||
		stage.MutationID != req.MutationID || req.TargetDigest != hostbridge.TargetDigest("github", githubStageOperation, target) {
		return nil, effectgateway.ErrAdmissionRejected
	}
	return rotation.NewPrepareActionWithFence(ctx, req.MutationID, r.Plan, stage.Authorize,
		func(ctx context.Context, _ rotation.PreparationPlan, fence func(context.Context) error) (journal.Observation, error) {
			var fenceErr error
			outcome, err := stage.Client.CreateRepositoryGenerationFrom(ctx, t, func(ctx context.Context) ([]byte, error) {
				return stage.Load(ctx, stage.SourceHandle)
			}, func(ctx context.Context) error {
				fenceErr = fence(ctx)
				return fenceErr
			})
			if fenceErr != nil {
				return journal.Observation{}, fenceErr
			}
			switch {
			case outcome == gh.SecretNotWritten:
				return journal.Observation{}, rotation.ErrActionNotCurrent
			case outcome != gh.SecretWriteAcknowledged || err != nil:
				return journal.Observation{}, journal.ErrUnknownEffect
			default:
				return journal.Observation{State: journal.Acknowledged, ProofDigest: hostbridge.OperationDigest(req.MutationID, req.TargetDigest)}, nil
			}
		})
}

func (r GitHubRotation) retire(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
	if r.Retirement == nil {
		return nil, effectgateway.ErrAdmissionRejected
	}
	retirement := *r.Retirement
	t := retirement.Target
	target, err := providerlifecycle.GitHubGenerationTarget(t.Repository, t.SecretName, t.RepositoryID, t.OwnerID, r.Plan.Old.Number)
	if err != nil || !strings.EqualFold(r.Plan.Old.Target, t.Repository) || retirement.MutationID != req.MutationID ||
		req.TargetDigest != hostbridge.TargetDigest("github", githubDeleteOperation, target) {
		return nil, effectgateway.ErrAdmissionRejected
	}
	return rotation.NewFinalizeAction(ctx, req.MutationID, r.Plan, retirement.Activation, retirement.Consumers, time.Now(),
		retirement.VerifyActivation, retirement.VerifyConsumer, retirement.Authorize, t.Verify,
		func(ctx context.Context, old rotation.Generation, handle string) (journal.Observation, error) {
			result, err := retirement.Client.DeleteGitHubGenerationSecret(ctx, func(_ context.Context, provider, operation, canonical string) error {
				if provider != "github" || operation != githubDeleteOperation || canonical != target || handle != t.SecretName {
					return effectgateway.ErrAdmissionRejected
				}
				return nil
			}, t.Repository, handle, t.RepositoryID, t.OwnerID, old.Number)
			return journal.Observation{State: journal.Acknowledged, ProofDigest: result.ProofDigest}, err
		})
}
