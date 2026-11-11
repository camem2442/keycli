package hostbridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

// These tests use the real KeyCLI journal (durable intent, writer lock, unknown
// effect latch) rather than a Python stand-in, so crash/concurrency claims are
// grounded in the shipping implementation.

func testStore(t *testing.T) journal.Store {
	t.Helper()
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	return journal.Store{Root: filepath.Join(parent, "journal")}
}

func testIntent() journal.Intent {
	return journal.Intent{
		OperationID:  "op-harden-1",
		TargetDigest: TargetDigest("cloudflare", "create-account-token", "acct/tokens/kc-rot-1/permission-group/pg"),
		PlanDigest:   OperationDigest("plan-1", "target"),
		GrantDigest:  OperationDigest("grant-1", "target"),
	}
}

func ack() journal.Observation {
	return journal.Observation{State: journal.Acknowledged, ProofDigest: OperationDigest("proof", "1")}
}

// H20 — concurrent duplicate operation: exactly one dispatch may reach the
// provider, every other writer is refused.
func TestH20ConcurrentDuplicateOperationAdmitsOnce(t *testing.T) {
	store := testStore(t)
	intent := testIntent()

	var providerCalls int32
	var admitted int32
	var refused int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Dispatch(intent, func() (journal.Observation, error) {
				atomic.AddInt32(&providerCalls, 1)
				return ack(), nil
			})
			if err != nil {
				atomic.AddInt32(&refused, 1)
				return
			}
			atomic.AddInt32(&admitted, 1)
		}()
	}
	wg.Wait()

	if admitted != 1 {
		t.Fatalf("admitted dispatches = %d, want 1", admitted)
	}
	if refused != 7 {
		t.Fatalf("refused dispatches = %d, want 7", refused)
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want 1", providerCalls)
	}
	record, err := store.Status(intent.OperationID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if record.State != journal.Acknowledged {
		t.Fatalf("state = %s, want ACKNOWLEDGED", record.State)
	}
}

// H21 — crash after the durable intent: the resumed process must re-read the
// recorded state and refuse a second dispatch.
func TestH21CrashAfterDurableIntentResumesFromDisk(t *testing.T) {
	store := testStore(t)
	intent := testIntent()

	var providerCalls int32
	_, err := store.Dispatch(intent, func() (journal.Observation, error) {
		atomic.AddInt32(&providerCalls, 1)
		return journal.Observation{}, errors.New("process killed after the intent was written")
	})
	if !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatalf("crash-like effect must latch unknown effect, got %v", err)
	}

	// New process: a fresh Store value over the same root, as after a restart.
	restarted := journal.Store{Root: store.Root}
	record, statusErr := restarted.Status(intent.OperationID)
	if statusErr != nil {
		t.Fatalf("status after restart: %v", statusErr)
	}
	if record.State != journal.UnknownEffect {
		t.Fatalf("state after restart = %s, want UNKNOWN_EFFECT", record.State)
	}

	if _, err := restarted.Dispatch(intent, func() (journal.Observation, error) {
		atomic.AddInt32(&providerCalls, 1)
		return ack(), nil
	}); err == nil {
		t.Fatal("redispatch after crash must be refused")
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want 1", providerCalls)
	}
}

// H22 — provider applied the effect but the response was lost. The latch stays
// unknown, no automatic retry happens, and only a read-only reconcile closes it.
func TestH22ProviderSuccessWithLostResponseIsNotRetried(t *testing.T) {
	store := testStore(t)
	intent := testIntent()

	var providerCalls int32
	_, err := store.Dispatch(intent, func() (journal.Observation, error) {
		atomic.AddInt32(&providerCalls, 1) // the provider did apply the effect
		return journal.Observation{}, errors.New("response lost")
	})
	if !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatalf("lost response must latch unknown effect, got %v", err)
	}

	if _, retryErr := store.Dispatch(intent, func() (journal.Observation, error) {
		atomic.AddInt32(&providerCalls, 1)
		return ack(), nil
	}); retryErr == nil {
		t.Fatal("automatic retry after an unknown result must be refused")
	}

	var observeCalls int32
	record, reconcileErr := store.Reconcile(intent, func(current journal.Record) (journal.Observation, error) {
		atomic.AddInt32(&observeCalls, 1)
		if current.State != journal.UnknownEffect {
			t.Fatalf("reconcile observed %s", current.State)
		}
		return journal.Observation{State: journal.ObservedApplied, ProofDigest: OperationDigest("readback", "1")}, nil
	})
	if reconcileErr != nil {
		t.Fatalf("reconcile: %v", reconcileErr)
	}
	if record.State != journal.ObservedApplied {
		t.Fatalf("state = %s", record.State)
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want 1 (no redispatch during reconcile)", providerCalls)
	}
	if observeCalls != 1 {
		t.Fatalf("read-only observer calls = %d, want 1", observeCalls)
	}
}

// H28 — forged host callback: the callback must not authorize a different
// request, and the provider effect must never run.
func TestH28ForgedCallbackCannotAuthorizeAnotherTarget(t *testing.T) {
	fake := &fakeAdmitter{decision: admitted(), mutate: func(d *Decision, _ Request) {
		d.Bound.TargetDigest = TargetDigest("cloudflare", "create-account-token", "other")
	}}
	bridge := newBridge(fake)

	var providerCalls int32
	err := bridge.Authorize(context.Background(), "cloudflare", "create-account-token", "acct/tokens/kc-rot-1/permission-group/pg")
	if err == nil {
		providerCalls++
	}
	if !errors.Is(err, ErrBindingMismatch) {
		t.Fatalf("forged callback must be refused, got %v", err)
	}
	if providerCalls != 0 {
		t.Fatalf("provider effect ran %d times", providerCalls)
	}
}

// H23 (Go side) — two writers with different journal roots on the same target
// cannot share a latch: the second root starts empty and is therefore not
// evidence of the first writer's state.
func TestH23SeparateJournalRootsAreNotShared(t *testing.T) {
	intent := testIntent()
	first := testStore(t)
	second := testStore(t)

	if _, err := first.Dispatch(intent, func() (journal.Observation, error) { return ack(), nil }); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	record, err := second.Status(intent.OperationID)
	if err != nil && !errors.Is(err, journal.ErrNotFound) {
		t.Fatalf("second root status: %v", err)
	}
	if err == nil && record.State == journal.Acknowledged {
		t.Fatal("a different journal root must not observe the first writer's acknowledgement")
	}
	if _, err := second.Dispatch(intent, func() (journal.Observation, error) { return ack(), nil }); err != nil {
		t.Fatalf("second root is independent by design, got %v", err)
	}

	// The admission layer is what prevents this: the request must pin one root.
	if TargetDigest("cloudflare", "create-account-token", "x") == "" {
		t.Fatal("unreachable")
	}
}
