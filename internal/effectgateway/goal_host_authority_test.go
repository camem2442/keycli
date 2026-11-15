package effectgateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
)

var testNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type fakeAdmitter struct{ last hostbridge.Request }

func (f *fakeAdmitter) Admit(req hostbridge.Request) (hostbridge.Decision, error) {
	f.last = req
	return hostbridge.Decision{
		Status: hostbridge.StatusAdmitted, Reason: "GRANT_AND_CLAIM_MATCH", GrantID: "g-keycli-1",
		AuthorizesExecution: true, ProviderMutation: true, ExpiresAt: testNow.Add(2 * time.Minute), Bound: req,
	}, nil
}

// The REAL HostBridge, Gateway and Journal are connected to a synthetic local provider.
type r0Claims struct{ snapshot effectgateway.ClaimSnapshot }

func (s *r0Claims) Snapshot(_ context.Context, _ string) (effectgateway.ClaimSnapshot, error) {
	return s.snapshot, nil
}

type r0Provider struct{ calls int }

func (p *r0Provider) Apply(_ context.Context, _ effectgateway.Request) (effectgateway.EffectResult, error) {
	p.calls++
	return effectgateway.EffectResult{
		State:       effectgateway.StateAcknowledged,
		ProofDigest: strings.Repeat("d", 64),
	}, nil
}

type r0HostCheck struct {
	active bool
	calls  int
	failAt int
}

func (l *r0HostCheck) check(_ context.Context, _ hostbridge.GatewayIntent) error {
	l.calls++
	if !l.active || (l.failAt > 0 && l.calls >= l.failAt) {
		return errors.New("current mutation lease or Grant not qualified")
	}
	return nil
}

type r0Fixture struct {
	gw       *effectgateway.Gateway
	req      effectgateway.Request
	key      []byte
	binding  *hostbridge.GatewayBinding
	provider *r0Provider
	admitter *fakeAdmitter
	lease    *r0HostCheck
}

func makeR0(t *testing.T) *r0Fixture {
	t.Helper()
	id := hostbridge.Identity{
		GoalID: "goal-r0", OperationID: "op-delivery-1", MutationID: "op-delivery-1",
		Repository: "camem2442/keycli", HeadSHA: strings.Repeat("a", 40), BaseSHA: strings.Repeat("b", 40),
		WriterGeneration: 3, WriterOwner: "opencode-local", WriterSession: "ses-1",
		HostIdentity: "opencode-local", CredentialIsolation: "separate-uid", IsolationDigest: strings.Repeat("c", 64),
	}
	raw := "0123456789abcdef0123456789abcdef/tokens/kc-rot-1/permission-group/0123456789abcdef0123456789abcdef"
	req := effectgateway.Request{
		GoalID: id.GoalID, MutationID: id.OperationID,
		Provider: "cloudflare", Operation: "create-account-token",
		Action: "issue", TargetDigest: hostbridge.TargetDigest("cloudflare", "create-account-token", raw),
		PlanDigest: strings.Repeat("a", 64), GrantDigest: strings.Repeat("b", 64),
		Generation: id.WriterGeneration, Owner: "opencode-local", Session: "ses-1",
		ExpiresAt: testNow.Add(time.Minute),
	}
	keys := effectgateway.NewMemoryKeyRing(10*time.Minute, func() time.Time { return testNow })
	key, err := keys.Issue(req.GoalID, req.Owner, req.Session, req.Generation)
	if err != nil {
		t.Fatal(err)
	}
	admitter := &fakeAdmitter{}
	lease := &r0HostCheck{active: true}
	binding := &hostbridge.GatewayBinding{
		Bridge: &hostbridge.Bridge{Admitter: admitter, Identity: id, Now: func() time.Time { return testNow }}, RawTarget: raw,
		ExpectedHead: id.HeadSHA, PlanDigest: req.PlanDigest,
		GrantDigest: req.GrantDigest, Owner: req.Owner, Session: req.Session,
		CheckCurrent: lease.check,
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &r0Provider{}
	gw := &effectgateway.Gateway{
		Claims: &r0Claims{snapshot: effectgateway.ClaimSnapshot{
			GoalID: req.GoalID, Generation: req.Generation,
			Session: req.Session, Owner: req.Owner, State: "ACTIVE",
			LeaseExpiresAt: testNow.Add(2 * time.Minute),
		}},
		Keys: keys, Authority: effectgateway.GoalHostAuthority{Binding: binding},
		Journal:  journal.Store{Root: filepath.Join(parent, "journal")},
		Provider: provider,
		Now:      func() time.Time { return testNow },
	}
	return &r0Fixture{gw: gw, req: req, key: key, binding: binding,
		provider: provider, admitter: admitter, lease: lease}
}
func (f *r0Fixture) submit() (effectgateway.Receipt, error) {
	return f.gw.Submit(context.Background(), effectgateway.Sign(f.req, f.key))
}
func TestR0BoundGatewayRealBridgeAppliesOnceAndReplay(t *testing.T) {
	f := makeR0(t)
	first, err := f.submit()
	if err != nil || first.Status != effectgateway.StatusApplied ||
		first.ProviderCalls != 1 || f.provider.calls != 1 || f.lease.calls != 2 {
		t.Fatalf("first=%+v err=%v provider=%d lease=%d", first, err, f.provider.calls, f.lease.calls)
	}
	if f.admitter.last.TargetDigest != f.req.TargetDigest {
		t.Fatalf("host target digest differs from signed request: %+v", f.admitter.last)
	}
	second, err := f.submit()
	if err != nil || second.Status != effectgateway.StatusDuplicate ||
		second.ProviderCalls != 0 || f.provider.calls != 1 {
		t.Fatalf("duplicate=%+v err=%v calls=%d", second, err, f.provider.calls)
	}
}
func TestR0BindingNegativesNeverReachProvider(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*r0Fixture)
	}{
		{"operation-name", func(f *r0Fixture) { f.req.Operation = "issue" }},
		{"action-class", func(f *r0Fixture) { f.req.Action = "revoke" }},
		{"target-identity", func(f *r0Fixture) { f.req.TargetDigest = strings.Repeat("e", 64) }},
		{"mutation-id", func(f *r0Fixture) { f.req.MutationID = "op-other" }},
		{"plan-digest", func(f *r0Fixture) { f.req.PlanDigest = strings.Repeat("e", 64) }},
		{"grant-digest", func(f *r0Fixture) { f.req.GrantDigest = strings.Repeat("e", 64) }},
		{"wrong-owner", func(f *r0Fixture) { f.req.Owner = "another-owner" }},
		{"wrong-session", func(f *r0Fixture) { f.req.Session = "another-session" }},
		{"stale-generation", func(f *r0Fixture) { f.req.Generation++ }},
		{"stale-source", func(f *r0Fixture) { f.binding.ExpectedHead = strings.Repeat("f", 40) }},
		{"unqualified-lease", func(f *r0Fixture) { f.lease.active = false }},
		{"no-host-check", func(f *r0Fixture) { f.binding.CheckCurrent = nil }},
		{"no-raw-target", func(f *r0Fixture) { f.binding.RawTarget = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := makeR0(t)
			tc.mutate(f)
			receipt, err := f.submit()
			if err == nil || f.provider.calls != 0 {
				t.Fatalf("expected rejection and no provider call: receipt=%+v err=%v calls=%d", receipt, err, f.provider.calls)
			}
		})
	}
}
func TestR0MutationLeaseChangesAtEffectBoundary(t *testing.T) {
	f := makeR0(t)
	f.lease.failAt = 2
	receipt, err := f.submit()
	if err != nil || receipt.Status != effectgateway.StatusNoEffect ||
		f.provider.calls != 0 || f.lease.calls != 2 {
		t.Fatalf("stale lease must cause NO_EFFECT: receipt=%+v err=%v calls=%d lease=%d",
			receipt, err, f.provider.calls, f.lease.calls)
	}
}
func TestR0SignedEnvelopeTamperingRefused(t *testing.T) {
	f := makeR0(t)
	env := effectgateway.Sign(f.req, f.key)
	env.Request.PlanDigest = strings.Repeat("e", 64)
	_, err := f.gw.Submit(context.Background(), env)
	if !errors.Is(err, effectgateway.ErrUnauthenticated) || f.provider.calls != 0 ||
		f.lease.calls != 0 {
		t.Fatalf("tampered envelope not rejected before host check: %v", err)
	}
}
func TestR0RejectsOperationalConfiguration(t *testing.T) {
	if effectgateway.CurrentLock().Channel == "operational" {
		t.Skip("operational build requires separate acceptance")
	}
	t.Setenv(effectgateway.ProviderEffectsEnv, "enabled")
	f := makeR0(t)
	f.gw.Operational = true
	f.req.Operation = ""
	_, err := f.submit()
	if !errors.Is(err, effectgateway.ErrNotConfigured) ||
		f.provider.calls != 0 || f.lease.calls != 0 {
		t.Fatalf("dev effect passed build lock: %v calls=%d lease=%d", err, f.provider.calls, f.lease.calls)
	}
}

func TestR0SignedOperationThroughHTTP(t *testing.T) {
	f := makeR0(t)
	body, err := json.Marshal(effectgateway.Sign(f.req, f.key))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	f.gw.Handler(time.Second).ServeHTTP(response, req)
	if response.Code != http.StatusOK || f.provider.calls != 1 {
		t.Fatalf("status=%d response=%s calls=%d", response.Code, response.Body, f.provider.calls)
	}
}
