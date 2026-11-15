// Package githubdelivery connects an admitted GitHub Secret effect to the
// durable delivery journal. It is intended for a trusted host, not CLI input.
package githubdelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/delivery"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/target/gh"
)

// Grant is the decision returned by the host's authoritative authorization
// callback. Its digest refers to independently verified host grant evidence.
type Grant struct {
	PlanDigest, GrantDigest                                        string
	RepositoryID, RepositoryOwnerID, EnvironmentID, OrganizationID int64
	ExpiresAt                                                      time.Time
}

type Authorize func(context.Context, string, delivery.DeliveryPlan) (Grant, error)
type LoadSecret func(context.Context, delivery.SecretRef) ([]byte, error)

type secretWriter interface {
	PutSecretFrom(context.Context, gh.SecretRESTTarget, func(context.Context) ([]byte, error)) (gh.SecretWriteOutcome, error)
}

// Run admits an exact plan before touching the journal or resolving the source.
// load must return an owned buffer for the adapter to clear.
func Run(ctx context.Context, store journal.Store, id string, plan delivery.DeliveryPlan, authorize Authorize, load LoadSecret, client *gh.SecretRESTClient) (journal.Record, error) {
	if client == nil {
		return journal.Record{}, errors.New("GitHub Secret client is required")
	}
	return run(ctx, store, id, plan, authorize, load, client)
}

func run(ctx context.Context, store journal.Store, id string, plan delivery.DeliveryPlan, authorize Authorize, load LoadSecret, client secretWriter) (journal.Record, error) {
	if authorize == nil || load == nil || plan.Provider != "github-actions-secret" || plan.Effect != "immediate-secret-update" ||
		plan.Source == nil || plan.Authoritative || plan.AuthorizesExecution || plan.AcceptanceClaim {
		return journal.Record{}, errors.New("invalid GitHub delivery plan or host binding")
	}
	source := *plan.Source
	target := plan.Target
	target.SelectedRepositoryIDs = slices.Clone(target.SelectedRepositoryIDs)
	plan.Target.SelectedRepositoryIDs = slices.Clone(target.SelectedRepositoryIDs)
	plan.Source = &delivery.SecretRef{Alias: source.Alias, CanonicalName: source.CanonicalName}
	plan.Unverified = slices.Clone(plan.Unverified)
	planID := plan.ID
	plan.ID = ""
	data, err := json.Marshal(plan)
	if err != nil || digest(data) != planID {
		return journal.Record{}, errors.New("GitHub delivery plan identity mismatch")
	}
	plan.ID = planID
	grant, err := authorize(ctx, id, plan)
	if err != nil || grant.PlanDigest != planID || ctx.Err() != nil || !time.Now().Before(grant.ExpiresAt) {
		return journal.Record{}, errors.New("GitHub delivery host admission failed")
	}
	ctx, cancel := context.WithDeadline(ctx, grant.ExpiresAt)
	defer cancel()
	targetDigest, err := gitHubTargetDigest(target, grant)
	if err != nil {
		return journal.Record{}, err
	}
	remote := gh.SecretRESTTarget{
		Scope: target.Scope, Repository: target.Repository, Environment: target.Environment,
		Organization: target.Organization, Destination: target.Destination, SelectedRepositoryIDs: target.SelectedRepositoryIDs,
		RepositoryID: grant.RepositoryID, RepositoryOwnerID: grant.RepositoryOwnerID, EnvironmentID: grant.EnvironmentID,
		OrganizationID: grant.OrganizationID,
	}
	intent := journal.Intent{OperationID: id, TargetDigest: targetDigest, PlanDigest: planID, GrantDigest: grant.GrantDigest}
	record, err := store.Dispatch(intent, func() (journal.Observation, error) {
		outcome, err := client.PutSecretFrom(ctx, remote, func(ctx context.Context) ([]byte, error) { return load(ctx, source) })
		var state journal.State
		switch {
		case outcome == gh.SecretNotWritten:
			state = journal.ObservedNoEffect
		case outcome == gh.SecretWriteAcknowledged && err == nil:
			state = journal.Acknowledged
		default:
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		// This digest references the local adapter observation. It does not
		// attest the secret value or enroll a verification receipt.
		evidence, _ := json.Marshal(struct {
			Intent journal.Intent
			State  journal.State
		}{intent, state})
		return journal.Observation{State: state, ProofDigest: digest(evidence)}, nil
	})
	if err == nil && record.State == journal.ObservedNoEffect {
		err = errors.New("GitHub Secret write was not attempted")
	}
	return record, err
}

func gitHubTargetDigest(target delivery.TargetRef, grant Grant) (string, error) {
	// Repository selection and spelling are not resource identity. For example
	// changing the visibility list cannot bypass a pending organization write.
	var parent, child int64
	switch target.Scope {
	case "repository":
		parent = grant.RepositoryID
	case "environment":
		parent, child = grant.RepositoryID, grant.EnvironmentID
	case "organization":
		parent = grant.OrganizationID
	default:
		return "", errors.New("invalid GitHub Secret scope")
	}
	if parent <= 0 || (target.Scope == "environment" && child <= 0) || target.Destination == "" {
		return "", errors.New("immutable GitHub Secret target is required")
	}
	data, _ := json.Marshal(struct {
		Provider, Scope string
		Parent, Child   int64
		Destination     string
	}{"github-actions-secret", target.Scope, parent, child, strings.ToUpper(target.Destination)})
	return digest(data), nil
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
