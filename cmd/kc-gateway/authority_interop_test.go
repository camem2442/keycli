//go:build darwin || linux

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
)

// Set KEYCLI_TEST_WORKSPACE_INFRA to a workspace-infra checkout containing
// the preview methods and their Python fixture, then run with -count=1.
func TestGatewayPythonAuthorityPreviews(t *testing.T) {
	for _, tc := range []struct{ name, note string }{
		{"unchanged-head", "EXECUTION_FENCE_UNAVAILABLE"},
		{"changed-head", "EXECUTION_FENCE_REJECTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runGatewayPythonAuthority(t, "authority_preview.py", tc.name, effectgateway.Receipt{
				Status: effectgateway.StatusNoEffect, OperationState: string(journal.ObservedNoEffect), Note: tc.note,
			}, false)
		})
	}
}

func TestGatewayPythonGuardedExecution(t *testing.T) {
	for _, tc := range []struct {
		name    string
		receipt effectgateway.Receipt
	}{
		{"current", effectgateway.Receipt{Status: effectgateway.StatusApplied, OperationState: string(journal.Acknowledged), ProviderCalls: 1, AuthoritySettlement: effectgateway.SettlementDone}},
		{"revoked", effectgateway.Receipt{Status: effectgateway.StatusNoEffect, OperationState: string(journal.ObservedNoEffect), Note: "EXECUTION_FENCE_REJECTED", AuthoritySettlement: effectgateway.SettlementFailed}},
		{"lost-reply", effectgateway.Receipt{Status: effectgateway.StatusUnknown, OperationState: string(journal.UnknownEffect)}},
		{"lost-admission-reply", effectgateway.Receipt{Status: effectgateway.StatusDuplicate}},
		{"lost-finish-reply", effectgateway.Receipt{Status: effectgateway.StatusApplied, OperationState: string(journal.Acknowledged), ProviderCalls: 1, AuthoritySettlement: effectgateway.SettlementUnconfirmed}},
		{"unobserved-ack", effectgateway.Receipt{Status: effectgateway.StatusApplied, OperationState: string(journal.Acknowledged), ProviderCalls: 1, AuthoritySettlement: effectgateway.SettlementUnconfirmed}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runGatewayPythonAuthority(t, "authority_guarded.py", tc.name, tc.receipt, true)
		})
	}
}

func TestGatewayPythonDurableAuthority(t *testing.T) {
	t.Run("current", func(t *testing.T) {
		runGatewayPythonAuthority(t, "authority_durable.py", "current", effectgateway.Receipt{
			Status: effectgateway.StatusApplied, OperationState: string(journal.Acknowledged),
			ProviderCalls: 1, AuthoritySettlement: effectgateway.SettlementDone,
		}, true)
	})
	t.Run("lost-reply", func(t *testing.T) {
		runGatewayPythonAuthority(t, "authority_durable.py", "lost-reply", effectgateway.Receipt{
			Status: effectgateway.StatusUnknown, OperationState: string(journal.UnknownEffect),
		}, true)
	})
}

func TestGatewayPythonAuthorityCLI(t *testing.T) {
	t.Run("current", func(t *testing.T) {
		runGatewayPythonAuthority(t, "authority_cli.py", "current", effectgateway.Receipt{
			Status: effectgateway.StatusApplied, OperationState: string(journal.Acknowledged),
			ProviderCalls: 1, AuthoritySettlement: effectgateway.SettlementDone,
		}, true)
	})
	t.Run("restart-unknown", func(t *testing.T) {
		runGatewayPythonAuthority(t, "authority_cli.py", "restart-unknown", effectgateway.Receipt{
			Status: effectgateway.StatusUnknown, OperationState: string(journal.UnknownEffect),
		}, true)
	})
}

func runGatewayPythonAuthority(t *testing.T, filename, mode string, expected effectgateway.Receipt, pinPID bool) {
	t.Helper()
	infra := os.Getenv("KEYCLI_TEST_WORKSPACE_INFRA")
	if infra == "" {
		t.Skip("set KEYCLI_TEST_WORKSPACE_INFRA to the workspace-infra preview checkout")
	}
	infra, err := filepath.Abs(infra)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", filename))
	if err != nil {
		t.Fatal(err)
	}
	dir := gatewayTempDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-B", script, dir, mode)
	cmd.Dir = infra
	fixture, err := filepath.Abs("../../internal/effectgateway/testdata")
	if err != nil {
		t.Fatal(err)
	}
	cmd.Env = append(os.Environ(), "PYTHONPATH="+infra+string(os.PathListSeparator)+filepath.Join(infra, "tests")+string(os.PathListSeparator)+fixture, "TMPDIR="+dir)
	cmd.Stderr = os.Stderr
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
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	var ready struct {
		DB      string                      `json:"db"`
		Config  string                      `json:"config"`
		Binding hostbridge.AuthorityBinding `json:"binding"`
	}
	decoder := json.NewDecoder(stdout)
	if err := decoder.Decode(&ready); err != nil {
		t.Fatalf("Python Authority did not become ready: %v", err)
	}
	runGatewayProcessSubmit(t, dir, ready.DB, ready.Config, ready.Binding, expected, mode == "lost-admission-reply", func(pid int) {
		if pinPID {
			if _, err := fmt.Fprintln(stdin, pid); err != nil {
				t.Fatal(err)
			}
			if filename == "authority_cli.py" {
				var peerPinned bool
				if err := decoder.Decode(&peerPinned); err != nil || !peerPinned {
					t.Fatalf("activated Authority CLI did not receive the gateway PID: %v", err)
				}
			}
			return
		}
		if err := stdin.Close(); err != nil {
			t.Fatal(err)
		}
	})
	if pinPID {
		if _, err := fmt.Fprintln(stdin, "complete"); err != nil {
			t.Fatal(err)
		}
		if err := stdin.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Python Authority fixture: %v", err)
	}
}
