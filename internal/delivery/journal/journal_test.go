package journal

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
)

func testIntent(id string) Intent {
	return Intent{
		OperationID:  id,
		TargetDigest: strings.Repeat("a", 64),
		PlanDigest:   strings.Repeat("b", 64),
		GrantDigest:  strings.Repeat("c", 64),
	}
}

func proof(state State) Observation {
	return Observation{State: state, ProofDigest: strings.Repeat("d", 64)}
}

func newTestStore(t *testing.T) Store {
	t.Helper()
	// Darwin can expose the same temporary directory through a system alias.
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	return Store{Root: filepath.Join(parent, "journal")}
}

func TestDispatchRecordsAcknowledgementAndRejectsReplay(t *testing.T) {
	store := newTestStore(t)
	intent := testIntent("one")
	if _, err := store.Status(intent.OperationID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new status: %v", err)
	}
	if _, err := os.Lstat(store.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("status created journal: %v", err)
	}
	record, err := store.Dispatch(intent, func() (Observation, error) {
		raw, err := os.ReadFile(filepath.Join(store.Root, "one.json"))
		if err != nil || !strings.Contains(string(raw), `"state":"DISPATCHED"`) {
			t.Fatalf("effect started without dispatch record: %v", err)
		}
		current, err := store.Status(intent.OperationID)
		if err != nil || current.State != UnknownEffect {
			t.Fatalf("in-flight status = %v, %v", current, err)
		}
		return proof(Acknowledged), nil
	})
	if err != nil || record.State != Acknowledged || record.Intent != intent {
		t.Fatalf("dispatch = %v, %v", record, err)
	}
	persisted, err := store.Status(intent.OperationID)
	if err != nil || persisted != record {
		t.Fatalf("persisted = %v, %v", persisted, err)
	}
	const v1 = `{"v":1,"operation_id":"one","target_digest":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","plan_digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","grant_digest":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","state":"ACKNOWLEDGED","proof_digest":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}`
	if raw, err := os.ReadFile(filepath.Join(store.Root, "one.json")); err != nil || string(raw) != v1 {
		t.Fatalf("legacy journal bytes changed: %s, %v", raw, err)
	}
	if _, err := store.Dispatch(intent, neverEffect(t)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("replay: %v", err)
	}
	if next, err := store.Dispatch(testIntent("two"), func() (Observation, error) {
		return proof(ObservedNoEffect), nil
	}); err != nil || next.State != ObservedNoEffect {
		t.Fatalf("settled target: %v, %v", next, err)
	}
}

func TestBoundDispatchRequiresOriginalIntent(t *testing.T) {
	store := newTestStore(t)
	intent := testIntent("bound")
	binding := authoritybinding.Binding{
		GoalID: "goal", MutationID: intent.OperationID, WriterGeneration: 1,
		Owner: "owner", Session: "session", Provider: "cloudflare", Operation: "registered",
		ResourceDigest: strings.Repeat("f", 64), TargetDigest: intent.TargetDigest,
		PlanDigest: intent.PlanDigest, GrantDigest: intent.GrantDigest,
	}
	wrongIntent := intent
	wrongIntent.TargetDigest = binding.ResourceDigest
	if _, err := store.DispatchBound(wrongIntent, binding, neverBoundEffect(t)); err == nil {
		t.Fatal("accepted inconsistent original intent")
	}
	path := filepath.Join(store.Root, intent.OperationID+".json")
	_, err := store.DispatchBound(intent, binding, func(DispatchGuard) (Observation, error) {
		dir, err := store.open(false)
		if err != nil {
			t.Fatal(err)
		}
		defer dir.Close()
		record, err := readRecord(dir, intent.OperationID)
		if err != nil || record.Version != 2 || record.State != Dispatched ||
			record.Binding == nil || *record.Binding != binding || record.Intent != intent {
			t.Fatalf("exact dispatch marker missing: %+v, %v", record, err)
		}
		return Observation{}, errors.New("synthetic lost response")
	})
	if !errors.Is(err, ErrUnknownEffect) {
		t.Fatalf("unresolved binding: %v", err)
	}
	if _, err := store.DispatchBound(intent, binding, neverBoundEffect(t)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("original replay: %v", err)
	}
	if _, err := store.Dispatch(intent, neverEffect(t)); !errors.Is(err, ErrIntentMismatch) {
		t.Fatalf("legacy replay of bound operation: %v", err)
	}
	if _, err := store.Reconcile(intent, func(Record) (Observation, error) {
		t.Fatal("legacy observer called for bound operation")
		return Observation{}, nil
	}); !errors.Is(err, ErrIntentMismatch) {
		t.Fatalf("legacy reconciliation of bound operation: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Change only the journal's copy of the target; the Authority copy is intact.
	edited := strings.Replace(string(raw), intent.TargetDigest, binding.ResourceDigest, 1)
	if edited == string(raw) {
		t.Fatal("test did not edit the persisted intent")
	}
	if err := os.WriteFile(path, []byte(edited), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Status(intent.OperationID); err == nil {
		t.Fatal("read inconsistent bound operation")
	}
	if _, err := store.DispatchBound(intent, binding, neverBoundEffect(t)); err == nil || errors.Is(err, ErrDuplicate) {
		t.Fatalf("inconsistent original classified as a replay: %v", err)
	}
}

func TestUnknownRequiresExactReconciliation(t *testing.T) {
	store := newTestStore(t)
	intent := testIntent("one")
	const internalDetail = "untrusted-callback-detail"
	record, err := store.Dispatch(intent, func() (Observation, error) {
		return proof(Acknowledged), errors.New(internalDetail)
	})
	if record.State != UnknownEffect || !errors.Is(err, ErrUnknownEffect) ||
		record.ProofDigest != "" || strings.Contains(err.Error(), internalDetail) {
		t.Fatalf("uncertain dispatch = %v, %v", record, err)
	}
	next := testIntent("two")
	next.PlanDigest = strings.Repeat("e", 64)
	if _, err := store.Dispatch(next, neverEffect(t)); !errors.Is(err, ErrTargetPending) {
		t.Fatalf("unresolved target: %v", err)
	}
	unrelated := testIntent("other")
	unrelated.TargetDigest = strings.Repeat("f", 64)
	if _, err := store.Dispatch(unrelated, func() (Observation, error) { return proof(Acknowledged), nil }); err != nil {
		t.Fatalf("independent target: %v", err)
	}
	mismatch := intent
	mismatch.GrantDigest = next.PlanDigest
	if _, err := store.Reconcile(mismatch, func(Record) (Observation, error) {
		t.Fatal("observer called with the wrong grant")
		return Observation{}, nil
	}); err == nil {
		t.Fatal("accepted wrong grant")
	}
	if _, err := store.Reconcile(intent, func(Record) (Observation, error) {
		return proof(Acknowledged), nil
	}); !errors.Is(err, ErrUnknownEffect) {
		t.Fatalf("accepted acknowledgement during reconciliation: %v", err)
	}
	verified, err := store.Reconcile(intent, func(record Record) (Observation, error) {
		if record.State != UnknownEffect || record.Intent != intent {
			t.Fatalf("observed wrong intent: %v", record)
		}
		return proof(ObservedApplied), nil
	})
	if err != nil || verified.State != ObservedApplied {
		t.Fatalf("reconcile = %v, %v", verified, err)
	}
	if _, err := store.Dispatch(next, func() (Observation, error) { return proof(Acknowledged), nil }); err != nil {
		t.Fatalf("reconciled target remains blocked: %v", err)
	}
}

func TestOrphanedIntentCannotDispatch(t *testing.T) {
	store := newTestStore(t)
	intent := testIntent("one")
	dir, _, unlock, err := store.lock(true)
	if err != nil {
		t.Fatal(err)
	}
	err = writeRecord(dir, Record{Version: 1, Intent: intent, State: IntentWritten})
	unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Dispatch(intent, neverEffect(t)); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("orphaned intent replay: %v", err)
	}
	if _, err := store.Dispatch(testIntent("two"), neverEffect(t)); !errors.Is(err, ErrTargetPending) {
		t.Fatalf("orphaned intent target: %v", err)
	}
	if _, err := store.Reconcile(intent, func(Record) (Observation, error) {
		return proof(ObservedApplied), nil
	}); !errors.Is(err, ErrUnknownEffect) {
		t.Fatalf("intent-only record accepted as applied: %v", err)
	}
	record, err := store.Reconcile(intent, func(Record) (Observation, error) {
		return proof(ObservedNoEffect), nil
	})
	if err != nil || record.State != ObservedNoEffect {
		t.Fatalf("reconcile orphaned intent: %v, %v", record, err)
	}
}

func TestCrossProcessLockAndCrash(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestJournalProcess$")
	cmd.Env = append(os.Environ(), "KC_JOURNAL_TEST_ROOT="+store.Root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	var ready [1]byte
	if _, err := io.ReadFull(stdout, ready[:]); err != nil || ready[0] != 'r' {
		t.Fatalf("journal subprocess did not dispatch: %v", err)
	}
	intent := testIntent("child")
	next := testIntent("two")
	if _, err := store.Dispatch(next, neverEffect(t)); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent dispatch: %v", err)
	}
	if _, err := store.Reconcile(intent, func(Record) (Observation, error) {
		t.Fatal("observer raced an active dispatch")
		return Observation{}, nil
	}); !errors.Is(err, ErrBusy) {
		t.Fatalf("concurrent reconciliation: %v", err)
	}
	// Closing stdin causes the child to exit inside the callback, without
	// settling or running its deferred unlock.
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 23 {
		t.Fatalf("unexpected child exit: %v", err)
	}
	record, err := store.Status(intent.OperationID)
	if err != nil || record.State != UnknownEffect {
		t.Fatalf("abandoned dispatch = %v, %v", record, err)
	}
	if _, err := store.Dispatch(next, neverEffect(t)); !errors.Is(err, ErrTargetPending) {
		t.Fatalf("abandoned target latch: %v", err)
	}
}

func TestJournalProcess(t *testing.T) {
	root := os.Getenv("KC_JOURNAL_TEST_ROOT")
	if root == "" {
		return
	}
	store := Store{Root: root}
	_, err := store.Dispatch(testIntent("child"), func() (Observation, error) {
		_, _ = os.Stdout.Write([]byte{'r'})
		var release [1]byte
		_, _ = os.Stdin.Read(release[:])
		os.Exit(23)
		return Observation{}, nil
	})
	t.Fatalf("child did not enter dispatch: %v", err)
}

func TestUnsafeStorageFailsBeforeEffect(t *testing.T) {
	t.Run("path traversal", func(t *testing.T) {
		store := newTestStore(t)
		if _, err := store.Dispatch(testIntent("../escape"), neverEffect(t)); err == nil {
			t.Fatal("accepted path traversal")
		}
	})
	t.Run("root symlink", func(t *testing.T) {
		store := newTestStore(t)
		if err := os.Symlink(filepath.Dir(store.Root), store.Root); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Dispatch(testIntent("one"), neverEffect(t)); err == nil {
			t.Fatal("accepted symlink root")
		}
	})
	t.Run("lock symlink", func(t *testing.T) {
		store := newTestStore(t)
		if err := os.Mkdir(store.Root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("elsewhere", filepath.Join(store.Root, ".lock")); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Dispatch(testIntent("one"), neverEffect(t)); err == nil {
			t.Fatal("accepted symlink lock")
		}
	})
	t.Run("corrupt journal", func(t *testing.T) {
		store := newTestStore(t)
		if err := os.Mkdir(store.Root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(store.Root, "another.json"), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Dispatch(testIntent("one"), neverEffect(t)); err == nil {
			t.Fatal("accepted corrupt target index")
		}
	})
}

func neverEffect(t *testing.T) func() (Observation, error) {
	t.Helper()
	return func() (Observation, error) {
		t.Fatal("unexpected provider effect")
		return Observation{}, nil
	}
}

func neverBoundEffect(t *testing.T) func(DispatchGuard) (Observation, error) {
	return func(DispatchGuard) (Observation, error) { return neverEffect(t)() }
}
