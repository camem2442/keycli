package cloudflaredelivery

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/target/cloudflare"
)

const acct = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const worker = "fixture-worker"
const env = "staging"
const dep = "00000000-0000-4000-8000-000000000001"
const previous = "11111111-1111-4111-8111-111111111111"
const next = "22222222-2222-4222-8222-222222222222"
const op = "op-123"

type fakeObserver struct {
	before, after cloudflare.DeploymentSnapshot
	tag           string
	badBinding    bool
	reads         int
	badActive     bool
	errorOnPost   bool
}

func (f *fakeObserver) Observe(_ context.Context, account, worker string) (cloudflare.DeploymentSnapshot, error) {
	f.reads++
	if f.reads > 1 {
		if f.errorOnPost {
			return cloudflare.DeploymentSnapshot{}, errors.New("provider down")
		}
		return f.after, nil
	}
	return f.before, nil
}
func (f *fakeObserver) GetVersion(_ context.Context, account, worker, id string) (cloudflare.VersionMetadata, error) {
	if f.badBinding {
		return cloudflare.VersionMetadata{ID: id, Tag: f.tag}, nil
	}
	return cloudflare.VersionMetadata{ID: id, Tag: f.tag, BindingNames: []string{"OPENAI_API_KEY"}}, nil
}
func fixture(t *testing.T) (delivery.DeliveryPlan, Grant, cloudflare.VersionStageRequest, *fakeObserver) {
	t.Helper()
	plan := delivery.DeliveryPlan{Version: 1, Profile: "fixture", Provider: "cloudflare-workers", Effect: "inactive-version", Target: delivery.TargetRef{AccountAlias: "dev", Worker: worker, Environment: env, Destination: "OPENAI_API_KEY"}, Source: &delivery.SecretRef{Alias: "openai_worker", CanonicalName: "openai_worker"}, RemoteVerification: "NOT VERIFIED"}
	p := plan
	p.ID = ""
	raw, _ := json.Marshal(p)
	plan.ID = digest(raw)
	before := cloudflare.DeploymentSnapshot{AccountID: acct, Worker: worker, DeploymentID: dep, Versions: []cloudflare.VersionPercentage{{VersionID: previous, Percentage: 100}}, LatestVersionIDs: []string{previous}}
	after := before
	after.LatestVersionIDs = []string{next, previous}
	gr := Grant{PlanDigest: plan.ID, GrantDigest: strings.Repeat("a", 64), AccountID: acct, Worker: worker, Environment: env, Destination: "OPENAI_API_KEY", ExpectedActiveDigest: ActiveDigest(before), ExpiresAt: time.Now().Add(time.Hour)}
	req := cloudflare.VersionStageRequest{AccountID: acct, Worker: worker, Environment: env, SecretName: "OPENAI_API_KEY"}
	return plan, gr, req, &fakeObserver{before: before, after: after, tag: "kc-" + op}
}
func permit(g Grant) Authorize {
	return func(context.Context, string, delivery.DeliveryPlan) (Grant, error) { return g, nil }
}
func TestRunBindsInactiveVersionAndJournal(t *testing.T) {
	plan, grant, req, remote := fixture(t)
	called := 0
	secretCalls := 0
	stage := func(_ context.Context, r cloudflare.VersionStageRequest, load func(context.Context) ([]byte, error)) (cloudflare.VersionStageResult, error) {
		called++
		if r.VersionTag != "kc-"+op {
			t.Fatalf("missing host operation tag: %s", r.VersionTag)
		}
		value, e := load(context.Background())
		if e != nil || string(value) != "synthetic" {
			t.Fatal("missing source")
		}
		return cloudflare.VersionStageResult{State: cloudflare.VersionAcknowledgedUnverified}, nil
	}
	result, err := run(context.Background(), journal.Store{}, op, plan, permit(grant), func(context.Context, delivery.SecretRef) ([]byte, error) {
		secretCalls++
		return []byte("synthetic"), nil
	}, req, remote, stage)
	if err != nil || result.Record.State != journal.Acknowledged || result.VersionID != next || result.ProofDigest == "" || called != 1 || secretCalls != 1 || remote.reads != 2 {
		t.Fatalf("unexpected stage %+v, %v, called=%d secret=%d reads=%d", result, err, called, secretCalls, remote.reads)
	}
}
func TestRunDeniesInvalidPlanBeforeProviderOrSecret(t *testing.T) {
	plan, grant, req, remote := fixture(t)
	plan.ID = "wrong"
	called := false
	out, err := run(context.Background(), journal.Store{}, op, plan, permit(grant), func(context.Context, delivery.SecretRef) ([]byte, error) { called = true; return nil, nil }, req, remote, func(context.Context, cloudflare.VersionStageRequest, func(context.Context) ([]byte, error)) (cloudflare.VersionStageResult, error) {
		called = true
		return cloudflare.VersionStageResult{}, nil
	})
	if err == nil || called || remote.reads != 0 || out.Record.State != "" {
		t.Fatalf("untrusted plan progressed: %+v %v", out, err)
	}
}
func TestRunBlocksUnboundHostTarget(t *testing.T) {
	plan, grant, req, remote := fixture(t)
	grant.AccountID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	stageCalled := false
	_, err := run(context.Background(), journal.Store{}, op, plan, permit(grant), func(context.Context, delivery.SecretRef) ([]byte, error) { stageCalled = true; return nil, nil }, req, remote, func(context.Context, cloudflare.VersionStageRequest, func(context.Context) ([]byte, error)) (cloudflare.VersionStageResult, error) {
		stageCalled = true
		return cloudflare.VersionStageResult{}, nil
	})
	if err == nil || stageCalled || remote.reads != 0 {
		t.Fatal("wrong account could stage")
	}
}
func TestRunUnknownWhenDeploymentChangesOrVersionCannotBeBound(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeObserver)
	}{
		{"traffic", func(f *fakeObserver) {
			f.after.Versions = []cloudflare.VersionPercentage{{VersionID: next, Percentage: 100}}
		}},
		{"wrong tag", func(f *fakeObserver) { f.tag = "kc-somebody-else" }},
		{"missing binding", func(f *fakeObserver) { f.badBinding = true }},
		{"provider unavailable", func(f *fakeObserver) { f.errorOnPost = true }},
		{"two candidate versions", func(f *fakeObserver) {
			f.after.LatestVersionIDs = []string{next, "33333333-3333-4333-8333-333333333333", previous}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan, grant, req, remote := fixture(t)
			tt.mutate(remote)
			outcome, err := run(context.Background(), journal.Store{}, op, plan, permit(grant), func(context.Context, delivery.SecretRef) ([]byte, error) { return []byte("synthetic"), nil }, req, remote, func(context.Context, cloudflare.VersionStageRequest, func(context.Context) ([]byte, error)) (cloudflare.VersionStageResult, error) {
				return cloudflare.VersionStageResult{State: cloudflare.VersionAcknowledgedUnverified}, nil
			})
			if !errors.Is(err, journal.ErrUnknownEffect) || outcome.Record.State != journal.UnknownEffect || outcome.VersionID != "" {
				t.Fatalf("uncertain remote effect accepted %+v %v", outcome, err)
			}
		})
	}
}
func TestRunUnknownOnWranglerFailure(t *testing.T) {
	plan, grant, req, remote := fixture(t)
	outcome, err := run(context.Background(), journal.Store{}, op, plan, permit(grant), func(context.Context, delivery.SecretRef) ([]byte, error) { return []byte("synthetic"), nil }, req, remote, func(context.Context, cloudflare.VersionStageRequest, func(context.Context) ([]byte, error)) (cloudflare.VersionStageResult, error) {
		return cloudflare.VersionStageResult{State: cloudflare.VersionUnknownEffect}, errors.New("failed")
	})
	if !errors.Is(err, journal.ErrUnknownEffect) || outcome.Record.State != journal.UnknownEffect || remote.reads != 1 {
		t.Fatal("failed effect was not preserved as unknown")
	}
}
func TestReconcileFindsTaggedVersionWithoutCallingWrangler(t *testing.T) {
	plan, grant, _, remote := fixture(t)
	remote.before = remote.after
	remote.reads = 0
	result, err := reconcile(context.Background(), journal.Store{}, op, plan, permit(grant), remote)
	if err != nil || result.State != journal.ObservedApplied || result.ProofDigest == "" || remote.reads != 1 {
		t.Fatalf("reconcile %+v %v", result, err)
	}
}
func TestReconcileUntrustedTagStaysUnknown(t *testing.T) {
	plan, grant, _, remote := fixture(t)
	remote.tag = "kc-other"
	remote.before = remote.after
	result, err := reconcile(context.Background(), journal.Store{}, op, plan, permit(grant), remote)
	if !errors.Is(err, journal.ErrUnknownEffect) || result.State != journal.UnknownEffect {
		t.Fatalf("unbound observation %+v %v", result, err)
	}
}
