package hostbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
)

var testNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type fakeAdmitter struct {
	calls    int
	last     Request
	decision Decision
	err      error
	mutate   func(*Decision, Request)
}

func (f *fakeAdmitter) Admit(req Request) (Decision, error) {
	f.calls++
	f.last = req
	if f.err != nil {
		return Decision{}, f.err
	}
	decision := f.decision
	decision.Bound = req
	if f.mutate != nil {
		f.mutate(&decision, req)
	}
	return decision, nil
}

func admitted() Decision {
	return Decision{
		Status:              StatusAdmitted,
		Reason:              "GRANT_AND_CLAIM_MATCH",
		GrantID:             "g-keycli-1",
		AuthorizesExecution: true,
		ProviderMutation:    true,
		ExpiresAt:           testNow.Add(2 * time.Minute),
	}
}

func testIdentity() Identity {
	return Identity{
		GoalID:              "KEYCLI-V010-SOURCE-LANDING-20261110",
		Repository:          "camem2442/keycli",
		HeadSHA:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BaseSHA:             "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		OperationID:         "op-delivery-1",
		WriterGeneration:    3,
		HostIdentity:        "opencode-local",
		Paths:               []string{"internal/rotation/finalize.go"},
		BudgetCost:          1,
		CredentialIsolation: "separate-uid",
		IsolationDigest:     "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}
}

func newBridge(admitter Admitter) *Bridge {
	return &Bridge{Admitter: admitter, Identity: testIdentity(), Now: func() time.Time { return testNow }}
}

func TestNilBridgeAndAdmitterRefuse(t *testing.T) {
	var nilBridge *Bridge
	if err := nilBridge.Authorize(context.Background(), "cloudflare", "create-account-token", "acct"); !errors.Is(err, ErrNoAdmitter) {
		t.Fatalf("nil bridge must refuse, got %v", err)
	}
	if err := newBridge(nil).Authorize(context.Background(), "cloudflare", "create-account-token", "acct"); !errors.Is(err, ErrNoAdmitter) {
		t.Fatalf("nil admitter must refuse, got %v", err)
	}
	if err := AuthorizeFunc(nil)(context.Background(), "cloudflare", "create-account-token", "acct"); !errors.Is(err, ErrNilBridge) {
		t.Fatalf("nil adapter must refuse, got %v", err)
	}
}

func TestUnknownOperationNeverReachesTheAdmitter(t *testing.T) {
	fake := &fakeAdmitter{decision: admitted()}
	bridge := newBridge(fake)
	for _, tc := range []struct{ provider, op string }{
		{"cloudflare", "create-worker"}, {"gitlab", "delete-exact-account-token"}, {"", "delete-exact-account-token"},
	} {
		if err := bridge.Authorize(context.Background(), tc.provider, tc.op, "target"); !errors.Is(err, ErrUnknownOperation) {
			t.Fatalf("%s/%s: expected unknown operation, got %v", tc.provider, tc.op, err)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("admitter must not run for unknown operations, calls=%d", fake.calls)
	}
}

func TestAdmittedMutationPassesExactBoundRequest(t *testing.T) {
	fake := &fakeAdmitter{decision: admitted()}
	bridge := newBridge(fake)
	if err := bridge.Authorize(context.Background(), "cloudflare", "create-account-token", "acct/tokens/kc-rot-1/permission-group/pg"); err != nil {
		t.Fatalf("admitted effect must pass: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("admitter calls = %d", fake.calls)
	}
	want := TargetDigest("cloudflare", "create-account-token", "acct/tokens/kc-rot-1/permission-group/pg")
	if fake.last.TargetDigest != want {
		t.Fatalf("target digest = %q want %q", fake.last.TargetDigest, want)
	}
	if fake.last.Action != ActionIssue || fake.last.Provider != "cloudflare" {
		t.Fatalf("unexpected request %+v", fake.last)
	}
	if fake.last.OperationDigest != OperationDigest(fake.last.OperationID, want) {
		t.Fatal("operation digest must bind the operation id to the target")
	}
	if fake.last.GoalID != testIdentity().GoalID || fake.last.WriterGeneration != 3 {
		t.Fatalf("host identity was not forwarded: %+v", fake.last)
	}
}

func TestBlockedDecisionIsRejected(t *testing.T) {
	fake := &fakeAdmitter{decision: Decision{Status: StatusBlocked, Reason: "PENDING_OPERATION_REQUIRES_RECONCILE"}}
	err := newBridge(fake).Authorize(context.Background(), "github", "delete-exact-repository-secret", "repo/secret")
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("expected rejection, got %v", err)
	}
}

func TestAdmitterErrorIsWrapped(t *testing.T) {
	fake := &fakeAdmitter{err: errors.New("grant store unreadable")}
	err := newBridge(fake).Authorize(context.Background(), "oracle", "stage-pending-vault-version", "secret/version")
	if !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("expected rejection, got %v", err)
	}
}

func TestDecisionForAnotherTargetIsRefused(t *testing.T) {
	fake := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) {
		d.Bound.TargetDigest = TargetDigest("cloudflare", "create-account-token", "other-target")
	}}
	err := newBridge(fake).Authorize(context.Background(), "cloudflare", "create-account-token", "acct/tokens/kc-rot-1/permission-group/pg")
	if !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("expected binding mismatch, got %v", err)
	}
}

func TestDecisionForAnotherOperationIsRefused(t *testing.T) {
	fake := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) {
		d.Bound.OperationID = "op-other"
	}}
	err := newBridge(fake).Authorize(context.Background(), "cloudflare", "create-account-token", "target")
	if !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("expected binding mismatch, got %v", err)
	}
}

func TestDecisionForAnotherHeadIsRefused(t *testing.T) {
	fake := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) {
		d.Bound.HeadSHA = "dddddddddddddddddddddddddddddddddddddddd"
	}}
	err := newBridge(fake).Authorize(context.Background(), "cloudflare", "create-account-token", "target")
	if !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("expected binding mismatch, got %v", err)
	}
}

func TestExpiredOrLongLivedDecisionIsRefused(t *testing.T) {
	expired := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) {
		d.ExpiresAt = testNow.Add(-time.Second)
	}}
	if err := newBridge(expired).Authorize(context.Background(), "cloudflare", "create-account-token", "target"); !errors.Is(err, ErrAdmissionInvalid) {
		t.Fatalf("expired decision must be refused, got %v", err)
	}
	long := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) {
		d.ExpiresAt = testNow.Add(MaxAdmissionLifetime + time.Second)
	}}
	if err := newBridge(long).Authorize(context.Background(), "cloudflare", "create-account-token", "target"); !errors.Is(err, ErrAdmissionInvalid) {
		t.Fatalf("over-long decision must be refused, got %v", err)
	}
}

func TestMutationClassAndGrantBindingAreRequired(t *testing.T) {
	wrongClass := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) {
		d.ProviderMutation = false
	}}
	if err := newBridge(wrongClass).Authorize(context.Background(), "cloudflare", "create-account-token", "target"); !errors.Is(err, ErrAdmissionInvalid) {
		t.Fatalf("mutation class mismatch must be refused, got %v", err)
	}

	noGrant := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) { d.GrantID = "" }}
	if err := newBridge(noGrant).Authorize(context.Background(), "cloudflare", "create-account-token", "target"); !errors.Is(err, ErrAdmissionInvalid) {
		t.Fatalf("missing grant binding must be refused, got %v", err)
	}

	notAuthorized := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) {
		d.AuthorizesExecution = false
	}}
	if err := newBridge(notAuthorized).Authorize(context.Background(), "cloudflare", "create-account-token", "target"); !errors.Is(err, ErrAdmissionInvalid) {
		t.Fatalf("non-authorising decision must be refused, got %v", err)
	}
}

func TestIncompleteHostIdentityIsRefused(t *testing.T) {
	fake := &fakeAdmitter{decision: admitted()}
	cases := map[string]func(*Identity){
		"empty goal":       func(i *Identity) { i.GoalID = "" },
		"bad repository":   func(i *Identity) { i.Repository = "keycli" },
		"short head":       func(i *Identity) { i.HeadSHA = "abc" },
		"head equals base": func(i *Identity) { i.HeadSHA = i.BaseSHA },
		"zero generation":  func(i *Identity) { i.WriterGeneration = 0 },
		"bad host":         func(i *Identity) { i.HostIdentity = "/Users/example" },
		"no isolation":     func(i *Identity) { i.CredentialIsolation = "" },
	}
	for name, mutate := range cases {
		identity := testIdentity()
		mutate(&identity)
		bridge := &Bridge{Admitter: fake, Identity: identity, Now: func() time.Time { return testNow }}
		if err := bridge.Authorize(context.Background(), "cloudflare", "create-account-token", "target"); !errors.Is(err, ErrAdmissionInvalid) {
			t.Fatalf("%s: expected invalid identity, got %v", name, err)
		}
	}
	if fake.calls != 0 {
		t.Fatalf("admitter must not run with an incomplete identity, calls=%d", fake.calls)
	}
}

func TestCancelledContextIsRefused(t *testing.T) {
	fake := &fakeAdmitter{decision: admitted()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newBridge(fake).Authorize(ctx, "cloudflare", "create-account-token", "target"); !errors.Is(err, ErrAdmissionInvalid) {
		t.Fatalf("cancelled context must refuse, got %v", err)
	}
	if fake.calls != 0 {
		t.Fatal("admitter must not run for a cancelled request")
	}
}

func TestClassifyOperationTable(t *testing.T) {
	cases := []struct {
		provider, operation, action string
		mutating                    bool
	}{
		{"cloudflare", "create-account-token", ActionIssue, true},
		{"cloudflare", "delete-exact-account-token", ActionRevoke, true},
		{"github", "delete-exact-repository-secret", ActionRevoke, true},
		{"github", "stage-repository-generation-secret", ActionStage, true},
		{"oracle", "stage-pending-vault-version", ActionStage, true},
		{"oracle", "schedule-deprecated-vault-version-deletion", ActionRevoke, true},
		{"synthetic", "record-local-test", ActionStage, true},
	}
	for _, tc := range cases {
		action, mutating, err := ClassifyOperation(tc.provider, tc.operation)
		if err != nil || action != tc.action || mutating != tc.mutating {
			t.Fatalf("%s/%s = %q %v %v", tc.provider, tc.operation, action, mutating, err)
		}
	}
	if _, _, err := ClassifyOperation("cloudflare", "purge-everything"); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("unlisted operation must be unknown, got %v", err)
	}
}

func TestBridgeSatisfiesProviderlifecycleHostAuthorize(t *testing.T) {
	var callback providerlifecycle.HostAuthorize = newBridge(&fakeAdmitter{decision: admitted()}).HostAuthorize()
	if err := callback(context.Background(), "cloudflare", "create-account-token", "target"); err != nil {
		t.Fatalf("callback must satisfy the provider lifecycle contract: %v", err)
	}
}

func TestDigestsAreDeterministicAndDistinct(t *testing.T) {
	a := TargetDigest("cloudflare", "create-account-token", "target-a")
	b := TargetDigest("cloudflare", "create-account-token", "target-b")
	if a == b {
		t.Fatal("different targets must not share a digest")
	}
	if a != TargetDigest("cloudflare", "create-account-token", "target-a") {
		t.Fatal("target digest must be deterministic")
	}
	if OperationDigest("op-1", a) == OperationDigest("op-2", a) {
		t.Fatal("operation digest must bind the operation id")
	}
}
