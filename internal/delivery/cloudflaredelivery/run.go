// Package cloudflaredelivery joins an authorized Cloudflare version-only effect
// to KeyCLI's persistent journal. This is a trusted-host API, not a CLI grant.
package cloudflaredelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/delivery"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/target/cloudflare"
)

// Grant is a *host-verified* binding; constructing this struct does not grant
// authority. The host must check its own real Goal/Claim and expiry first.
type Grant struct {
	PlanDigest  string
	GrantDigest string
	// OriginalWriteGrantDigest is used only by read-only Reconcile: the
	// exact digest persisted with the original write intent. It must not
	// also serve as the new read-authorization digest.
	OriginalWriteGrantDigest string
	AccountID                string
	Worker                   string
	Environment              string
	Destination              string
	ExpectedActiveDigest     string
	ExpiresAt                time.Time
}

type Authorize func(context.Context, string, delivery.DeliveryPlan) (Grant, error)
type LoadSecret func(context.Context, delivery.SecretRef) ([]byte, error)

type Observer interface {
	Observe(context.Context, string, string) (cloudflare.DeploymentSnapshot, error)
	GetVersion(context.Context, string, string, string) (cloudflare.VersionMetadata, error)
}
type stageFunc func(context.Context, cloudflare.VersionStageRequest, func(context.Context) ([]byte, error)) (cloudflare.VersionStageResult, error)

type Result struct {
	Record    journal.Record
	VersionID string
	// ProofDigest covers remote version+active distribution, not the secret value.
	ProofDigest string
}

// Run performs a single, host-authorized inactive version creation.
// All participating writers must also hold the owning workspace-infra Writer
// Claim. The local journal does not fence remote actors in other hosts.
func Run(ctx context.Context, store journal.Store, id string, plan delivery.DeliveryPlan, authorize Authorize, load LoadSecret, request cloudflare.VersionStageRequest, remote *cloudflare.VersionRESTObserver) (Result, error) {
	if remote == nil {
		return Result{}, errors.New("Cloudflare provider observer required")
	}
	return run(ctx, store, id, plan, authorize, load, request, remote, cloudflare.StageVersionSecret)
}

func run(ctx context.Context, store journal.Store, id string, plan delivery.DeliveryPlan, authorize Authorize, load LoadSecret, request cloudflare.VersionStageRequest, remote Observer, stage stageFunc) (Result, error) {
	if authorize == nil || load == nil || remote == nil || stage == nil || len(id) < 1 || len(id) > 96 || !validOpID(id) ||
		plan.Provider != "cloudflare-workers" || plan.Effect != "inactive-version" || plan.Source == nil ||
		plan.Authoritative || plan.AcceptanceClaim || plan.AuthorizesExecution ||
		plan.Target.Worker == "" || plan.Target.Environment == "" || plan.Target.Destination == "" || plan.Target.AccountAlias == "" {
		return Result{}, errors.New("invalid Cloudflare stage plan or trusted host binding")
	}
	// Clone caller-owned mutable data before hashing and authorization.
	source := *plan.Source
	plan.Source = &delivery.SecretRef{Alias: source.Alias, CanonicalName: source.CanonicalName}
	plan.Unverified = slices.Clone(plan.Unverified)
	planID := plan.ID
	plan.ID = ""
	canonical, err := json.Marshal(plan)
	if err != nil || digest(canonical) != planID {
		return Result{}, errors.New("Cloudflare stage plan digest mismatch")
	}
	plan.ID = planID
	grant, err := authorize(ctx, id, plan)
	if err != nil || ctx.Err() != nil || grant.PlanDigest != planID ||
		!time.Now().Before(grant.ExpiresAt) || !sha256Digest(grant.GrantDigest) ||
		!sha256Digest(grant.ExpectedActiveDigest) ||
		grant.Worker != plan.Target.Worker || grant.Environment != plan.Target.Environment ||
		grant.Destination != plan.Target.Destination ||
		grant.AccountID != request.AccountID || grant.Worker != request.Worker ||
		grant.Environment != request.Environment || grant.Destination != request.SecretName {
		return Result{}, errors.New("Cloudflare stage host admission or target mismatch")
	}
	if request.VersionTag != "" && request.VersionTag != "kc-"+id {
		return Result{}, errors.New("inconsistent stage operation tag")
	}
	request.VersionTag = "kc-" + id
	ctx, cancel := context.WithDeadline(ctx, grant.ExpiresAt)
	defer cancel()
	before, err := remote.Observe(ctx, grant.AccountID, grant.Worker)
	if err != nil {
		return Result{}, errors.New("Cloudflare pre-stage observation unavailable")
	}
	if before.AccountID != grant.AccountID || before.Worker != grant.Worker ||
		activeDigest(before) != grant.ExpectedActiveDigest || len(before.LatestVersionIDs) == 0 {
		return Result{}, errors.New("Cloudflare pre-stage active deployment drift")
	}
	if len(before.LatestVersionIDs) >= cloudflare.VersionInventoryLimit-1 {
		return Result{}, errors.New("Cloudflare version inventory has no room for a verifiable stage")
	}
	targetDigest := digest([]byte(strings.Join([]string{"cloudflare-workers", grant.AccountID, grant.Worker, grant.Environment, strings.ToUpper(grant.Destination)}, "\x00")))
	intent := journal.Intent{OperationID: id, TargetDigest: targetDigest, PlanDigest: planID, GrantDigest: grant.GrantDigest}
	result := Result{}
	notAttempted := false
	record, err := store.Dispatch(intent, func() (journal.Observation, error) {
		// Only resolve credential bytes inside durable journal Dispatch.
		stageResult, stageErr := stage(ctx, request, func(ctx context.Context) ([]byte, error) { return load(ctx, source) })
		if stageResult.State == cloudflare.VersionNotAttempted {
			notAttempted = true
			proof := digest([]byte(strings.Join([]string{"stage-not-attempted", id, targetDigest, planID, grant.GrantDigest}, "\x00")))
			return journal.Observation{State: journal.ObservedNoEffect, ProofDigest: proof}, nil
		}
		if stageErr != nil || stageResult.State != cloudflare.VersionAcknowledgedUnverified {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		versionID, proof, err := VerifyStageVersion(ctx, remote, before, grant.Destination, request.VersionTag, id, targetDigest)
		if err != nil {
			return journal.Observation{}, err
		}
		result.VersionID = versionID
		result.ProofDigest = proof
		return journal.Observation{State: journal.Acknowledged, ProofDigest: proof}, nil
	})
	result.Record = record
	if err != nil {
		result.VersionID = ""
		result.ProofDigest = ""
	}
	if err == nil && notAttempted {
		return result, errors.New("Cloudflare inactive-version stage did not start")
	}
	return result, err
}

// VerifyStageVersion observes the new version without submitting a mutation.
// Callers bind the pre-stage snapshot and the exact operation tag before launch.
func VerifyStageVersion(ctx context.Context, remote Observer, before cloudflare.DeploymentSnapshot, destination, tag, operationID, targetDigest string) (versionID, proof string, err error) {
	after, err := remote.Observe(ctx, before.AccountID, before.Worker)
	if err != nil || after.AccountID != before.AccountID || after.Worker != before.Worker ||
		activeDigest(before) == "" || activeDigest(after) != activeDigest(before) ||
		len(after.LatestVersionIDs) >= cloudflare.VersionInventoryLimit {
		return "", "", journal.ErrUnknownEffect
	}
	versionID, ok := singleNewVersion(before, after)
	if !ok {
		return "", "", journal.ErrUnknownEffect
	}
	detail, err := remote.GetVersion(ctx, before.AccountID, before.Worker, versionID)
	if err != nil || detail.ID != versionID || detail.Tag != tag || !slices.Contains(detail.BindingNames, destination) {
		return "", "", journal.ErrUnknownEffect
	}
	proof = digest([]byte(strings.Join([]string{operationID, targetDigest, versionID, activeDigest(after), detail.Tag}, "\x00")))
	return versionID, proof, nil
}

func singleNewVersion(before, after cloudflare.DeploymentSnapshot) (string, bool) {
	prev := make(map[string]bool, len(before.LatestVersionIDs))
	for _, id := range before.LatestVersionIDs {
		prev[id] = true
	}
	var newID string
	for _, id := range after.LatestVersionIDs {
		if !prev[id] {
			if newID != "" {
				return "", false
			}
			newID = id
		}
	}
	return newID, newID != ""
}

func activeDigest(s cloudflare.DeploymentSnapshot) string {
	if s.DeploymentID == "" || len(s.Versions) == 0 {
		return ""
	}
	vv := slices.Clone(s.Versions)
	slices.SortFunc(vv, func(a, b cloudflare.VersionPercentage) int { return strings.Compare(a.VersionID, b.VersionID) })
	data, err := json.Marshal(struct {
		ID       string
		Versions []cloudflare.VersionPercentage
	}{s.DeploymentID, vv})
	if err != nil {
		return ""
	}
	return digest(data)
}

// ActiveDigest is for the trusted host to bind an independently observed,
// exact Cloudflare active deployment distribution to an operation Grant.
func ActiveDigest(snapshot cloudflare.DeploymentSnapshot) string { return activeDigest(snapshot) }
func digest(data []byte) string                                  { v := sha256.Sum256(data); return hex.EncodeToString(v[:]) }
func sha256Digest(v string) bool {
	return len(v) == 64 && len(strings.Trim(v, "0123456789abcdef")) == 0
}
func validOpID(id string) bool {
	for _, r := range id {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func jsonEncode(v any) ([]byte, error) { return json.Marshal(v) }
func cloudflareAccount(v string) bool  { return regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(v) }
func digestTarget(g Grant) string {
	return digest([]byte(strings.Join([]string{"cloudflare-workers", g.AccountID, g.Worker, g.Environment, strings.ToUpper(g.Destination)}, "\x00")))
}
