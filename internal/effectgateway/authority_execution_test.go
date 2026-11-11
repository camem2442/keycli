//go:build darwin || linux

package effectgateway

import (
	"context"
	"encoding/json"
	"errors"
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
		want string
	}{
		{"current", "approved"},
		{"revoked", "GRANT_NOT_REGISTERED"},
		{"reservation-mismatch", "MUTATION_BINDING_MISMATCH"},
		{"journal-mismatch", "JOURNAL_INTENT_MISMATCH"},
		{"unheld-descriptor", "WRITER_LOCK_NOT_SHARED"},
		{"wrong-request-binding", "MUTATION_BINDING_MISMATCH"},
		{"wrong-reply-binding", errGuardedReply.Error()},
		{"wrong-reply-id", errGuardedReply.Error()},
		{"duplicate-reply-field", errGuardedReply.Error()},
		{"lost-reply", errGuardedNoReply.Error()},
		{"stalled-reply", errGuardedTimeout.Error()},
		{"wrong-peer", errGuardedPeer.Error()},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			h := newHarness(t)
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
				ExpiresAt: testNow.Add(time.Minute),
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
				State: "ACTIVE", LeaseExpiresAt: testNow.Add(time.Minute),
			}
			listener, socketPath := authoritySocket(t)
			done := make(chan error, 1)
			go func() { done <- runLocalAuthorityFixture(listener, binding, "admission only") }()

			verifierPath := filepath.Join(filepath.Dir(socketPath), "guard.sock")
			verifier := command("serve_guarded", stateRoot, h.root, tc.mode, verifierPath)
			events := processEvents(t, verifier)
			if err := verifier.Start(); err != nil {
				t.Fatal(err)
			}
			expectProcessEvent(t, events, "verifier-ready")

			a := localFixtureAuthority(t, socketPath, uint32(os.Geteuid()), reg, h.gw.Now)
			var guard journal.DispatchGuard
			var outcome string
			a.verifyBoundExecution = func(ctx context.Context, got hostbridge.AuthorityBinding, live journal.DispatchGuard) error {
				guard = live
				if got != binding {
					t.Fatalf("verifier received different binding: %+v", got)
				}
				return live.WithLockFile(func(lock *os.File) error {
					if tc.mode == "journal-mismatch" {
						path := filepath.Join(h.root, req.MutationID+".json")
						raw, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						var record journal.Record
						if err := json.Unmarshal(raw, &record); err != nil {
							t.Fatal(err)
						}
						record.Binding.Operation = "other-operation"
						edited, err := json.Marshal(record)
						if err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, edited, 0600); err != nil {
							t.Fatal(err)
						}
					}
					if tc.mode == "unheld-descriptor" {
						separate, err := os.OpenFile(filepath.Join(h.root, ".lock"), os.O_RDWR, 0)
						if err != nil {
							t.Fatal(err)
						}
						defer separate.Close()
						lock = separate
					}
					pid := verifier.Process.Pid
					if tc.mode == "wrong-peer" {
						pid++
					}
					if tc.mode == "wrong-request-binding" {
						got.Operation = "other-operation"
					}
					var exchangeErr error
					outcome, exchangeErr = exchangeGuardedExecution(ctx, verifierPath, uint32(os.Geteuid()), pid, got, lock)
					if errors.Is(exchangeErr, errGuardedTimeout) {
						_ = verifier.Process.Kill()
					}
					waitErr := verifier.Wait()
					if exchangeErr != nil {
						outcome = exchangeErr.Error()
						if waitErr != nil && !errors.Is(exchangeErr, errGuardedTimeout) {
							t.Fatalf("verifier exited after %v: %v", exchangeErr, waitErr)
						}
						return exchangeErr
					}
					if waitErr != nil {
						t.Fatalf("verifier did not finish: %v", waitErr)
					}
					if outcome != "approved" {
						return errors.New("synthetic execution denied")
					}
					return nil
				})
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

			receipt, err := h.gw.Submit(ctx, Sign(req, []byte(testKey)))
			if fixtureErr := <-done; fixtureErr != nil {
				t.Fatal(fixtureErr)
			}
			if err != nil || outcome != tc.want {
				t.Fatalf("verification outcome %q, want %q: %+v, %v", outcome, tc.want, receipt, err)
			}
			if err := guard.WithLockFile(func(*os.File) error {
				t.Fatal("settled dispatch guard remained usable")
				return nil
			}); err == nil {
				t.Fatal("settled dispatch guard returned success")
			}
			record, err := h.gw.Journal.Status(req.MutationID)
			if err != nil || record.Binding == nil || *record.Binding != binding {
				t.Fatalf("settled binding: %+v, %v", record, err)
			}
			if tc.want == "approved" {
				if calls != 1 || receipt.ProviderCalls != 1 || receipt.Status != StatusApplied || record.State != journal.Acknowledged {
					t.Fatalf("approved provider calls=%d receipt=%+v journal=%+v", calls, receipt, record)
				}
				if err := observer.Wait(); err != nil {
					t.Fatalf("observer did not see settlement after Go released the writer lock: %v", err)
				}
			} else if calls != 0 || receipt.ProviderCalls != 0 || receipt.Status != StatusNoEffect ||
				receipt.Note != "EXECUTION_FENCE_REJECTED" || record.State != journal.ObservedNoEffect {
				t.Fatalf("denied provider calls=%d receipt=%+v journal=%+v", calls, receipt, record)
			}
		})
	}
}
