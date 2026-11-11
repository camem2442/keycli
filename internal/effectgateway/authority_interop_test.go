//go:build darwin || linux

package effectgateway

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

type providerFunc func(context.Context, Request) (EffectResult, error)

func (f providerFunc) Apply(ctx context.Context, req Request) (EffectResult, error) {
	return f(ctx, req)
}

// Run with KEYCLI_TEST_WORKSPACE_INFRA pointed at a checkout containing the
// PR04 mutation coordinator and with go test -count=1.
func TestGatewayPythonTakeoverWaitsForJournal(t *testing.T) {
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

	h := newHarness(t)
	req := testRequest()
	req.Generation = 1
	h.claims.snapshot.Generation = req.Generation
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-B", script, h.root, string(payload))
	cmd.Dir = infra
	cmd.Env = append(os.Environ(), "PYTHONPATH="+infra)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdout.Close()
	defer func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	events := bufio.NewScanner(stdout)
	h.gw.Provider = providerFunc(func(context.Context, Request) (EffectResult, error) {
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		// Wait for Python to try the actual flock before letting Apply return.
		if !events.Scan() || events.Text() != "lock-contended" {
			t.Fatalf("takeover did not contend during Apply: event=%q err=%v", events.Text(), events.Err())
		}
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
