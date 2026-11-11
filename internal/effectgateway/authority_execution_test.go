//go:build darwin || linux

package effectgateway

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
)

// Run with KEYCLI_TEST_WORKSPACE_INFRA pointing at the PR04 fixtures.
func TestGatewayBoundPythonExecution(t *testing.T) {
	infra := os.Getenv("KEYCLI_TEST_WORKSPACE_INFRA")
	if infra == "" {
		t.Skip("set KEYCLI_TEST_WORKSPACE_INFRA to a checkout with the mutation coordinator")
	}
	infra, err := filepath.Abs(infra)
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/authority_execution.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		mode string
		want journal.State
		note string
	}{
		{"current", journal.Acknowledged, ""},
		{"revoked", journal.ObservedNoEffect, "EXECUTION_FENCE_REJECTED"},
		{"reservation-mismatch", journal.ObservedNoEffect, "EXECUTION_FENCE_REJECTED"},
		{"journal-mismatch", journal.ObservedNoEffect, "EXECUTION_FENCE_REJECTED"},
		{"unheld-descriptor", journal.ObservedNoEffect, "EXECUTION_FENCE_REJECTED"},
		{"wrong-request-binding", journal.ObservedNoEffect, "EXECUTION_FENCE_REJECTED"},
		{"wrong-reply-binding", journal.UnknownEffect, ""},
		{"wrong-reply-id", journal.UnknownEffect, ""},
		{"wrong-reply-expiry", journal.UnknownEffect, ""},
		{"duplicate-reply-field", journal.UnknownEffect, ""},
		{"unknown-verdict", journal.UnknownEffect, ""},
		{"lost-reply", journal.UnknownEffect, ""},
		{"stalled-reply", journal.UnknownEffect, ""},
		{"wrong-peer", journal.ObservedNoEffect, "AUTHORITY_UNAVAILABLE"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			h := newHarness(t)
			now := time.Now().UTC().Truncate(time.Second)
			h.gw.Now = time.Now
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := func(args ...string) *exec.Cmd {
				cmd := exec.CommandContext(ctx, "python3", append([]string{"-B", script}, args...)...)
				cmd.Dir = infra
				cmd.Env = append(os.Environ(), "PYTHONPATH="+infra+string(os.PathListSeparator)+filepath.Join(infra, "tests"))
				cmd.Stderr = os.Stderr
				return cmd
			}
			stateRoot := filepath.Join(filepath.Dir(h.root), "authority")
			if err := os.Mkdir(stateRoot, 0700); err != nil {
				t.Fatal(err)
			}
			raw, err := command("prepare", stateRoot, tc.mode).Output()
			if err != nil {
				t.Fatal(err)
			}
			var binding hostbridge.AuthorityBinding
			if err := json.Unmarshal(raw, &binding); err != nil {
				t.Fatal(err)
			}
			req := Request{
				GoalID: binding.GoalID, MutationID: binding.MutationID, Provider: binding.Provider, Action: "issue",
				TargetDigest: binding.TargetDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest,
				Generation: binding.WriterGeneration, Session: binding.Session, Owner: binding.Owner,
				ExpiresAt: now.Add(30 * time.Second),
			}
			reg := RegisteredOperation{
				Identity: hostbridge.Identity{
					GoalID: binding.GoalID, MutationID: binding.MutationID, OperationID: binding.Operation,
					WriterGeneration: binding.WriterGeneration, WriterOwner: binding.Owner, WriterSession: binding.Session,
				},
				Provider: binding.Provider, ProviderOperation: "create-account-token", CanonicalTarget: "account-id/token-family",
				ResourceDigest: binding.ResourceDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest,
			}
			h.claims.snapshot = ClaimSnapshot{
				GoalID: req.GoalID, Generation: req.Generation, Owner: req.Owner, Session: req.Session,
				State: "ACTIVE", LeaseExpiresAt: now.Add(30 * time.Second),
			}
			listener, socketPath := authoritySocket(t)

			verifierPath := filepath.Join(filepath.Dir(socketPath), "guard.sock")
			verifier := command("serve_guarded", stateRoot, h.root, tc.mode, verifierPath, req.ExpiresAt.Format(time.RFC3339Nano))
			events := processEvents(t, verifier)
			if err := verifier.Start(); err != nil {
				t.Fatal(err)
			}
			expectProcessEvent(t, events, "verifier-ready")

			a := localFixtureAuthority(t, socketPath, uint32(os.Geteuid()), reg, h.gw.Now)
			pid := verifier.Process.Pid
			if tc.mode == "wrong-peer" {
				pid++
			}
			a.Execution, err = hostbridge.NewGuardedExecution(verifierPath, uint32(os.Geteuid()), pid)
			if err != nil {
				t.Fatal(err)
			}
			h.gw.Authority = a
			calls := 0
			var observer *exec.Cmd
			h.gw.Provider = providerFunc(func(context.Context, Request) (EffectResult, error) {
				calls++
				observer = command("settlement", h.root, req.MutationID)
				events := processEvents(t, observer)
				if err := observer.Start(); err != nil {
					t.Fatal(err)
				}
				expectProcessEvent(t, events, "lock-contended")
				return EffectResult{State: StateAcknowledged, ProofDigest: req.PlanDigest}, nil
			})

			// Measure Authority response time after the Python verifier is ready.
			done := make(chan error, 1)
			go func() {
				err := runLocalAuthorityFixtureAt(listener, binding, "guarded", now)
				_ = listener.Close()
				done <- err
			}()
			receipt, err := h.gw.Submit(ctx, Sign(req, []byte(testKey)))
			if fixtureErr := <-done; fixtureErr != nil {
				t.Fatal(fixtureErr)
			}
			if err != nil {
				t.Fatalf("dispatch: %+v, %v", receipt, err)
			}
			if tc.mode == "stalled-reply" {
				_ = verifier.Process.Kill()
				_ = verifier.Wait()
			} else if err := verifier.Wait(); err != nil {
				t.Fatalf("verifier did not finish: %v", err)
			}
			record, err := h.gw.Journal.Status(req.MutationID)
			if err != nil || record.Binding == nil || *record.Binding != binding || record.State != tc.want {
				t.Fatalf("settled binding: %+v, %v", record, err)
			}
			switch tc.want {
			case journal.Acknowledged:
				if calls != 1 || receipt.ProviderCalls != 1 || receipt.Status != StatusApplied {
					t.Fatalf("approved provider calls=%d receipt=%+v journal=%+v", calls, receipt, record)
				}
				if err := observer.Wait(); err != nil {
					t.Fatalf("observer did not see settlement after Go released the writer lock: %v", err)
				}
			case journal.UnknownEffect:
				if calls != 0 || receipt.ProviderCalls != 0 || receipt.Status != StatusUnknown ||
					receipt.OperationState != string(journal.UnknownEffect) || receipt.ProofDigest != "" {
					t.Fatalf("ambiguous provider calls=%d receipt=%+v journal=%+v", calls, receipt, record)
				}
			case journal.ObservedNoEffect:
				if calls != 0 || receipt.ProviderCalls != 0 || receipt.Status != StatusNoEffect || receipt.Note != tc.note {
					t.Fatalf("denied provider calls=%d receipt=%+v journal=%+v", calls, receipt, record)
				}
			}
		})
	}
}
