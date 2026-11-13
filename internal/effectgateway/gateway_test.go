package effectgateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

var testNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

const (
	testKey    = "0123456789abcdef0123456789abcdef" // 32 bytes
	testGoal   = "KEYCLI-V010-SOURCE-LANDING-20261110"
	testOwner  = "opencode-local"
	testSess   = "ses-1"
	testTarget = "1111111111111111111111111111111111111111111111111111111111111111"
	testPlan   = "2222222222222222222222222222222222222222222222222222222222222222"
	testGrant  = "3333333333333333333333333333333333333333333333333333333333333333"
)

type fakeClaims struct {
	snapshot ClaimSnapshot
	err      error
	calls    int32
}

func (f *fakeClaims) Snapshot(context.Context, string) (ClaimSnapshot, error) {
	atomic.AddInt32(&f.calls, 1)
	if f.err != nil {
		return ClaimSnapshot{}, f.err
	}
	return f.snapshot, nil
}

type fakeKeys struct {
	key []byte
	err error
}

func (f *fakeKeys) Key(context.Context, string, string, string, int64) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.key, nil
}

type fakeBridge struct {
	err   error
	calls int32
	after func(int32)
}

func (f *fakeBridge) AuthorizeEffect(context.Context, Request) error {
	n := atomic.AddInt32(&f.calls, 1)
	if f.after != nil {
		f.after(n)
	}
	return f.err
}

type fakeProvider struct {
	calls  int32
	result EffectResult
	err    error
}

func (f *fakeProvider) Apply(context.Context, Request) (EffectResult, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.result, f.err
}

func activeClaim() ClaimSnapshot {
	return ClaimSnapshot{
		GoalID: testGoal, Generation: 7, Session: testSess, Owner: testOwner,
		State: "ACTIVE", LeaseExpiresAt: testNow.Add(2 * time.Minute),
	}
}

func testRequest() Request {
	return Request{
		GoalID: testGoal, MutationID: "op-s2-1", Provider: "cloudflare", Operation: "create-account-token", Action: "issue",
		TargetDigest: testTarget, PlanDigest: testPlan, GrantDigest: testGrant,
		Generation: 7, Session: testSess, Owner: testOwner,
		ExpiresAt: testNow.Add(time.Minute),
	}
}

type harness struct {
	gw       *Gateway
	claims   *fakeClaims
	keys     *fakeKeys
	bridge   *fakeBridge
	provider *fakeProvider
	root     string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "journal")
	h := &harness{
		claims:   &fakeClaims{snapshot: activeClaim()},
		keys:     &fakeKeys{key: []byte(testKey)},
		bridge:   &fakeBridge{},
		provider: &fakeProvider{result: EffectResult{State: StateAcknowledged, ProofDigest: testPlan}},
		root:     root,
	}
	h.gw = &Gateway{
		Claims: h.claims, Keys: h.keys, Bridge: h.bridge,
		Journal: journal.Store{Root: root}, Provider: h.provider, Synthetic: true,
		Now: func() time.Time { return testNow },
	}
	return h
}

func (h *harness) submit(t *testing.T) (Receipt, error) {
	t.Helper()
	return h.gw.Submit(context.Background(), Sign(testRequest(), []byte(testKey)))
}

// 1. normal intent → exactly one effect
func TestSubmitAppliesOnce(t *testing.T) {
	h := newHarness(t)
	receipt, err := h.submit(t)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if receipt.Status != StatusApplied || receipt.ProviderCalls != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
	if h.provider.calls != 1 || h.bridge.calls != 2 {
		t.Fatalf("provider=%d bridge=%d", h.provider.calls, h.bridge.calls)
	}
	record, err := h.gw.Journal.Status("op-s2-1")
	if err != nil || record.State != journal.Acknowledged {
		t.Fatalf("journal state = %v err=%v", record.State, err)
	}
}

// 2. duplicate mutation id → no additional effect
func TestDuplicateMutationHasNoAdditionalEffect(t *testing.T) {
	h := newHarness(t)
	if _, err := h.submit(t); err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.submit(t)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second.Status != StatusDuplicate || second.ProviderCalls != 0 {
		t.Fatalf("second = %+v", second)
	}
	if h.provider.calls != 1 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}
}

// 3. forged session / signature → unauthenticated, zero effects
func TestForgedSessionOrSignatureIsRejected(t *testing.T) {
	h := newHarness(t)
	env := Sign(testRequest(), []byte(testKey))
	env.Request.Session = "ses-attacker"
	if _, err := h.gw.Submit(context.Background(), env); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("tampered session: %v", err)
	}

	env2 := Sign(testRequest(), []byte("ffffffffffffffffffffffffffffffff"))
	if _, err := h.gw.Submit(context.Background(), env2); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("wrong key: %v", err)
	}

	h.keys.err = errors.New("no key for session")
	if _, err := h.submit(t); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing key: %v", err)
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}
}

// 4. stale generation → zero effects
func TestStaleGenerationRejected(t *testing.T) {
	h := newHarness(t)
	req := testRequest()
	req.Generation = 6
	_, err := h.gw.Submit(context.Background(), Sign(req, []byte(testKey)))
	if !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("got %v", err)
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}
}

// 5. expired request / expired lease / inactive claim → zero effects
func TestExpiredAndInactiveClaimRejected(t *testing.T) {
	h := newHarness(t)
	req := testRequest()
	req.ExpiresAt = testNow.Add(-time.Second)
	if _, err := h.gw.Submit(context.Background(), Sign(req, []byte(testKey))); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired request: %v", err)
	}

	h.claims.snapshot = activeClaim()
	h.claims.snapshot.LeaseExpiresAt = testNow.Add(-time.Minute)
	if _, err := h.submit(t); !errors.Is(err, ErrClaimLeaseExpired) {
		t.Fatalf("lease: %v", err)
	}

	h.claims.snapshot = activeClaim()
	h.claims.snapshot.State = "DRAINING"
	if _, err := h.submit(t); !errors.Is(err, ErrClaimNotActive) {
		t.Fatalf("state: %v", err)
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}
}

// 6. owner swap and target swap → rejected
func TestOwnerOrTargetSwapRejected(t *testing.T) {
	h := newHarness(t)
	h.claims.snapshot = activeClaim()
	h.claims.snapshot.Owner = "other-host"
	if _, err := h.submit(t); !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("owner: %v", err)
	}

	h.claims.snapshot = activeClaim()
	env := Sign(testRequest(), []byte(testKey))
	env.Request.TargetDigest = "4444444444444444444444444444444444444444444444444444444444444444"
	if _, err := h.gw.Submit(context.Background(), env); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("target swap: %v", err)
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}
}

// 7. eight concurrent requests → serialisation invariant holds
func TestConcurrentRequestsSerialise(t *testing.T) {
	h := newHarness(t)
	env := Sign(testRequest(), []byte(testKey))
	var applied, duplicate, busy, other int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := h.gw.Submit(context.Background(), env)
			switch {
			case err == nil && receipt.Status == StatusApplied:
				atomic.AddInt32(&applied, 1)
			case err == nil && receipt.Status == StatusDuplicate:
				atomic.AddInt32(&duplicate, 1)
			case errors.Is(err, ErrGatewayBusy):
				atomic.AddInt32(&busy, 1)
			default:
				atomic.AddInt32(&other, 1)
			}
		}()
	}
	wg.Wait()
	if applied != 1 {
		t.Fatalf("applied=%d duplicate=%d busy=%d other=%d", applied, duplicate, busy, other)
	}
	if h.provider.calls != 1 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}
	if duplicate+busy+other != 7 {
		t.Fatalf("losers=%d", duplicate+busy+other)
	}
}

// 8. crash before the provider call → no duplicate execution on retry
func TestCrashBeforeProviderDoesNotDoubleExecute(t *testing.T) {
	h := newHarness(t)
	intent := journal.Intent{OperationID: "op-s2-1", TargetDigest: testTarget, PlanDigest: testPlan, GrantDigest: testGrant}
	if _, err := h.gw.Journal.Dispatch(intent, func() (journal.Observation, error) {
		return journal.Observation{}, errors.New("gateway killed after durable intent")
	}); !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatalf("seed: %v", err)
	}

	receipt, err := h.submit(t)
	if err != nil {
		t.Fatalf("retry after crash: %v", err)
	}
	if receipt.Status != StatusDuplicate {
		t.Fatalf("receipt = %+v", receipt)
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}
}

// 9. provider applied, response lost → UNKNOWN_EFFECT, no auto retry
func TestLostResponseLatchesUnknownEffect(t *testing.T) {
	h := newHarness(t)
	h.provider.err = errors.New("connection reset after apply")
	receipt, err := h.submit(t)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if receipt.Status != StatusUnknown || receipt.ProviderCalls != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
	record, statusErr := h.gw.Journal.Status("op-s2-1")
	if statusErr != nil || record.State != journal.UnknownEffect {
		t.Fatalf("journal = %v err=%v", record.State, statusErr)
	}

	retry, err := h.submit(t)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if retry.Status != StatusDuplicate || h.provider.calls != 1 {
		t.Fatalf("retry = %+v calls=%d", retry, h.provider.calls)
	}
}

// 10. restart → no automatic redispatch
func TestRestartDoesNotRedispatch(t *testing.T) {
	h := newHarness(t)
	if _, err := h.submit(t); err != nil {
		t.Fatalf("submit: %v", err)
	}
	restarted := &Gateway{
		Claims: h.claims, Keys: h.keys, Bridge: h.bridge,
		Journal: journal.Store{Root: h.root}, Provider: h.provider, Synthetic: true,
		Now: func() time.Time { return testNow },
	}
	receipt, err := restarted.Submit(context.Background(), Sign(testRequest(), []byte(testKey)))
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	if receipt.Status != StatusDuplicate || h.provider.calls != 1 {
		t.Fatalf("restart = %+v calls=%d", receipt, h.provider.calls)
	}
}

// 11. reconcile is required; the gateway never settles an unknown result
func TestUnknownEffectRequiresReconcile(t *testing.T) {
	h := newHarness(t)
	h.provider.err = errors.New("timeout")
	if _, err := h.submit(t); err != nil {
		t.Fatalf("submit: %v", err)
	}
	record, err := h.gw.Journal.Status("op-s2-1")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if record.State != journal.UnknownEffect {
		t.Fatalf("auto-settled to %s", record.State)
	}
	if record.ProofDigest != "" {
		t.Fatalf("unknown effect recorded a proof: %s", record.ProofDigest)
	}
}

// 12. authority failure → fail closed, no journal write, no effect
func TestAuthorityUnavailableFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.claims.err = errors.New("claim store unreadable")
	if _, err := h.submit(t); !errors.Is(err, ErrAuthorityUnavail) {
		t.Fatalf("got %v", err)
	}
	if h.provider.calls != 0 || h.bridge.calls != 0 {
		t.Fatalf("provider=%d bridge=%d", h.provider.calls, h.bridge.calls)
	}
	if _, err := h.gw.Journal.Status("op-s2-1"); !errors.Is(err, journal.ErrNotFound) {
		t.Fatalf("journal must stay untouched, got %v", err)
	}
}

// Extra: admission rejection and no-effect result never claim applied
func TestAdmissionRejectionAndNoEffect(t *testing.T) {
	h := newHarness(t)
	h.bridge.err = errors.New("host admission rejected")
	if _, err := h.submit(t); !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("admission: %v", err)
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}

	h.bridge.err = nil
	h.provider.result = EffectResult{State: StateObservedNoEffect, ProofDigest: testGrant}
	receipt, err := h.submit(t)
	if err != nil {
		t.Fatalf("no-effect: %v", err)
	}
	if receipt.Status != StatusNoEffect || receipt.ProviderCalls != 1 {
		t.Fatalf("receipt = %+v", receipt)
	}
}

// Extra: effect-boundary re-check stops the provider call when the claim changed
func TestEffectBoundaryRecheckStopsProvider(t *testing.T) {
	h := newHarness(t)
	calls := int32(0)
	sc := &switchClaims{snapshot: activeClaim(), calls: &calls}
	h.gw.Claims = sc
	// First snapshot is ACTIVE, the second (inside the effect) is stale.
	receipt, err := h.submit(t)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if receipt.Status != StatusNoEffect || receipt.ProviderCalls != 0 {
		t.Fatalf("receipt = %+v", receipt)
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider calls = %d", h.provider.calls)
	}
}

func TestEffectBoundaryRejectsExpiredSignedIntentAfterHostAdmission(t *testing.T) {
	h := newHarness(t)
	now := testNow
	h.gw.Now = func() time.Time { return now }
	// Keep the Claim lease alive to isolate the signed request expiry.
	h.claims.snapshot.LeaseExpiresAt = testNow.Add(10 * time.Minute)
	h.bridge.after = func(n int32) {
		if n == 2 {
			// Time advances during the second trusted host re-admission.
			now = testNow.Add(2 * time.Minute)
		}
	}
	receipt, err := h.submit(t)
	if err != nil || receipt.Status != StatusNoEffect || receipt.ProviderCalls != 0 ||
		h.provider.calls != 0 || h.bridge.calls != 2 {
		t.Fatalf("expired intent reached provider: receipt=%+v err=%v calls=%d host=%d",
			receipt, err, h.provider.calls, h.bridge.calls)
	}
}

type switchClaims struct {
	snapshot ClaimSnapshot
	calls    *int32
}

func (s *switchClaims) Snapshot(context.Context, string) (ClaimSnapshot, error) {
	n := atomic.AddInt32(s.calls, 1)
	snap := s.snapshot
	if n >= 2 {
		snap.Generation = 8 // takeover between admission and effect
	}
	return snap, nil
}
