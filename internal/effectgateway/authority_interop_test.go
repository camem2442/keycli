//go:build darwin || linux

package effectgateway

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
)

func TestGatewayPythonReconcilesReservationWithoutDispatch(t *testing.T) {
	infra := os.Getenv("KEYCLI_TEST_WORKSPACE_INFRA")
	if infra == "" {
		t.Skip("set KEYCLI_TEST_WORKSPACE_INFRA to the Authority recovery checkout")
	}
	h := newHarness(t)
	h.gw.Now = time.Now
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	script, err := filepath.Abs("testdata/authority_no_dispatch.py")
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	socketRoot, err := os.MkdirTemp(tmp, "kc-reconcile-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketRoot) })
	endpoint := filepath.Join(socketRoot, "a.sock")
	cmd := exec.CommandContext(ctx, "python3", "-B", script, filepath.Join(filepath.Dir(h.root), "authority"), h.root, endpoint)
	cmd.Dir = infra
	cmd.Env = append(os.Environ(), "PYTHONPATH="+infra+string(os.PathListSeparator)+filepath.Join(infra, "tests"))
	cmd.Stderr = os.Stderr
	events := processEvents(t, cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var binding hostbridge.AuthorityBinding
	if !events.Scan() || json.Unmarshal(events.Bytes(), &binding) != nil || !binding.Valid() {
		t.Fatal("Authority did not enroll the fixture binding")
	}
	expiry := time.Now().Add(time.Minute)
	claim := ClaimSnapshot{GoalID: binding.GoalID, Owner: binding.Owner, Session: binding.Session, Generation: binding.WriterGeneration, State: "ACTIVE", LeaseExpiresAt: expiry}
	h.claims.snapshot = claim
	ring := NewMemoryKeyRing(time.Minute, time.Now)
	h.gw.Keys = ring
	key, err := ring.Issue(claim.GoalID, claim.Owner, claim.Session, claim.Generation)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	reg := RegisteredOperation{
		Identity: hostbridge.Identity{GoalID: binding.GoalID, OperationID: binding.Operation, MutationID: binding.MutationID,
			WriterGeneration: binding.WriterGeneration, WriterOwner: binding.Owner, WriterSession: binding.Session},
		Provider: binding.Provider, ProviderOperation: "create-account-token", CanonicalTarget: "account-id/token-family",
		ResourceDigest: binding.ResourceDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest,
	}
	local := localFixtureAuthority(t, endpoint, uint32(os.Geteuid()), reg, time.Now)
	local.Execution, err = hostbridge.NewGuardedExecution(endpoint, uint32(os.Geteuid()), cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	h.gw.Authority = local
	h.gw.RecoveryObserver = recoveryObserverFunc(func(context.Context, hostbridge.AuthorityBinding) (RecoveryEvidence, error) {
		t.Fatal("pre-dispatch cancellation must not consult the provider")
		return RecoveryEvidence{}, nil
	})
	req := Request{GoalID: binding.GoalID, MutationID: binding.MutationID, Provider: binding.Provider, Action: "issue",
		Owner: binding.Owner, Session: binding.Session, Generation: binding.WriterGeneration, ExpiresAt: expiry,
		TargetDigest: binding.TargetDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest}
	env := Sign(req, key)
	if _, err := h.gw.Submit(ctx, env); !errors.Is(err, ErrReservationUnknown) {
		t.Fatalf("expected an unconfirmed reservation: %v", err)
	}
	read := ReadRequest{GoalID: req.GoalID, MutationID: req.MutationID, Owner: req.Owner, Session: req.Session,
		Generation: req.Generation, ExpiresAt: expiry, Action: ReadStatus}
	statusKey := readGrantKey{read.GoalID, read.MutationID, read.Owner, read.Session, ReadStatus, read.Generation}
	reconcileKey := statusKey
	reconcileKey.Action = ReadReconcile
	grant := RecoveryGrant{Binding: binding, GrantDigest: strings.Repeat("8", 64), ProviderIdentityDigest: strings.Repeat("9", 64), ExpiresAt: expiry}
	h.gw.ReadAuthority = readGrantRegistry{statusKey: grant}
	if _, err := h.gw.Read(ctx, SignRead(read, key)); !errors.Is(err, ErrReadNotFound) {
		t.Fatalf("status should leave the missing journal unchanged: %v", err)
	}
	read.Action = ReadReconcile
	if _, err := h.gw.Read(ctx, SignRead(read, key)); !errors.Is(err, ErrReadNotFound) {
		t.Fatalf("status grant authorized reconciliation: %v", err)
	}
	h.gw.ReadAuthority = readGrantRegistry{statusKey: grant, reconcileKey: grant}
	for range 2 {
		got, err := h.gw.Read(ctx, SignRead(read, key))
		if err != nil || got.Status != StatusNoEffect || got.OperationState != string(journal.ObservedNoEffect) ||
			got.AuthoritySettlement != SettlementFailed || got.ProviderCalls != 0 || !journal.ValidDigest(got.ProofDigest) {
			t.Fatalf("reconcile: %+v, %v", got, err)
		}
	}
	record, err := h.gw.Journal.Status(binding.MutationID)
	if err != nil || record.Version != 2 || record.Binding == nil || *record.Binding != binding {
		t.Fatalf("unexpected recovery record: %+v, %v", record, err)
	}
	intent := record.Intent
	if _, err := h.gw.Journal.DispatchBound(intent, binding, func(journal.DispatchGuard) (journal.Observation, error) {
		t.Fatal("late dispatch crossed the cancellation record")
		return journal.Observation{}, nil
	}); !errors.Is(err, journal.ErrDuplicate) || h.provider.calls != 0 {
		t.Fatalf("late dispatch: calls=%d error=%v", h.provider.calls, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

type providerFunc func(context.Context, Request) (EffectResult, error)

func (f providerFunc) Apply(ctx context.Context, req Request) (EffectResult, error) {
	return f(ctx, req)
}

func pythonTakeover(t *testing.T, ctx context.Context, root string, req Request, mode string) *exec.Cmd {
	t.Helper()
	infra := os.Getenv("KEYCLI_TEST_WORKSPACE_INFRA")
	if infra == "" {
		t.Skip("set KEYCLI_TEST_WORKSPACE_INFRA to a checkout with the mutation coordinator")
	}
	infra, err := filepath.Abs(infra)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/authority_takeover.py")
	if err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "python3", "-B", script, root, string(payload), mode)
	cmd.Dir = infra
	cmd.Env = append(os.Environ(), "PYTHONPATH="+infra)
	cmd.Stderr = os.Stderr
	return cmd
}

func processEvents(t *testing.T, cmd *exec.Cmd) *bufio.Scanner {
	t.Helper()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = stdout.Close()
	})
	return bufio.NewScanner(stdout)
}

func expectProcessEvent(t *testing.T, events *bufio.Scanner, want string) {
	t.Helper()
	if !events.Scan() || events.Text() != want {
		t.Fatalf("waiting for %q: event=%q err=%v", want, events.Text(), events.Err())
	}
}

// Run with KEYCLI_TEST_WORKSPACE_INFRA pointed at a checkout containing the
// PR04 mutation coordinator and with go test -count=1.
func TestGatewayPythonTakeoverWaitsForJournal(t *testing.T) {
	h := newHarness(t)
	req := testRequest()
	req.Generation = 1
	h.claims.snapshot.Generation = req.Generation
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := pythonTakeover(t, ctx, h.root, req, "settled")
	events := processEvents(t, cmd)
	h.gw.Provider = providerFunc(func(context.Context, Request) (EffectResult, error) {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		// Wait for Python to try the actual flock before letting Apply return.
		expectProcessEvent(t, events, "lock-contended")
		return EffectResult{State: StateAcknowledged, ProofDigest: req.PlanDigest}, nil
	})

	receipt, err := h.gw.Submit(ctx, Sign(req, []byte(testKey)))
	if err != nil || receipt.ProviderCalls != 1 || receipt.OperationState != string(journal.Acknowledged) {
		t.Fatalf("synthetic dispatch: receipt=%+v err=%v", receipt, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Python takeover did not advance after journal settlement: %v", err)
	}
}

func TestGatewayPythonTakeoverAfterProviderCrash(t *testing.T) {
	h := newHarness(t)
	req := testRequest()
	req.Generation = 1
	h.claims.snapshot.Generation = req.Generation
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	python := pythonTakeover(t, ctx, h.root, req, "crash")
	pythonEvents := processEvents(t, python)

	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGatewayProviderCrashProcess$")
	child.Env = append(os.Environ(), "KC_GATEWAY_CRASH_TEST_ROOT="+h.root)
	child.Stderr = os.Stderr
	childEvents := processEvents(t, child)
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	expectProcessEvent(t, childEvents, "provider-entered")

	if err := python.Start(); err != nil {
		t.Fatal(err)
	}
	expectProcessEvent(t, pythonEvents, "lock-contended")
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := child.Wait(); !errors.As(err, &exit) ||
		exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("unexpected Gateway child exit: %v", err)
	}
	if err := python.Wait(); err != nil {
		t.Fatalf("Python takeover/recovery after Gateway crash: %v", err)
	}

	record, err := h.gw.Journal.Status(req.MutationID)
	if err != nil || record.State != journal.UnknownEffect || record.ProofDigest != "" {
		t.Fatalf("abandoned Gateway dispatch: record=%+v err=%v", record, err)
	}
	// Keep the Go claim fixture permissive so these checks exercise the
	// persisted journal independently of Python's newer claim generation.
	retry, err := h.gw.Submit(ctx, Sign(req, []byte(testKey)))
	if err != nil || retry.Status != StatusDuplicate || retry.ProviderCalls != 0 {
		t.Fatalf("same-mutation replay: receipt=%+v err=%v", retry, err)
	}
	req.MutationID = "same-target-after-crash"
	if _, err := h.gw.Submit(ctx, Sign(req, []byte(testKey))); !errors.Is(err, ErrTargetPending) {
		t.Fatalf("fresh mutation on unresolved target: %v", err)
	}
	if h.provider.calls != 0 {
		t.Fatalf("provider calls after Gateway crash: %d", h.provider.calls)
	}
}

func TestGatewayProviderCrashProcess(t *testing.T) {
	root := os.Getenv("KC_GATEWAY_CRASH_TEST_ROOT")
	if root == "" {
		return
	}
	h := newHarness(t)
	h.gw.Journal.Root = root
	req := testRequest()
	req.Generation = 1
	h.claims.snapshot.Generation = req.Generation
	h.gw.Provider = providerFunc(func(context.Context, Request) (EffectResult, error) {
		if _, err := fmt.Fprintln(os.Stdout, "provider-entered"); err != nil {
			t.Fatal(err)
		}
		var release [1]byte
		_, _ = io.ReadFull(os.Stdin, release[:])
		t.Fatal("provider resumed before the parent killed the Gateway process")
		return EffectResult{}, nil
	})
	receipt, err := h.gw.Submit(context.Background(), Sign(req, []byte(testKey)))
	t.Fatalf("Gateway returned from crash fixture: receipt=%+v err=%v", receipt, err)
}
