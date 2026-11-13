//go:build darwin || linux

package effectgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
)

type fixtureRegistry struct{ registration RegisteredOperation }

func (r fixtureRegistry) Resolve(context.Context, string, string) (RegisteredOperation, error) {
	return r.registration, nil
}

func authorityRegistration() (RegisteredOperation, Request) {
	req := testRequest()
	reg := RegisteredOperation{
		Identity: hostbridge.Identity{
			GoalID: req.GoalID, OperationID: "registered-operation-1", MutationID: req.MutationID,
			WriterGeneration: req.Generation, WriterOwner: req.Owner, WriterSession: req.Session,
		},
		Provider: req.Provider, ProviderOperation: "create-account-token", CanonicalTarget: "account-1/token-family-1",
		ResourceDigest: strings.Repeat("a", 64), PlanDigest: req.PlanDigest, GrantDigest: req.GrantDigest,
	}
	req.TargetDigest = hostbridge.TargetDigest(reg.Provider, reg.ProviderOperation, reg.CanonicalTarget)
	return reg, req
}

func authoritySocket(t *testing.T) (*net.UnixListener, string) {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "kc-authority-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	path := filepath.Join(root, "authority.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	return listener, path
}

func localFixtureAuthority(t *testing.T, path string, uid uint32, reg RegisteredOperation, now func() time.Time) LocalAuthority {
	t.Helper()
	client, err := hostbridge.NewLocalAdmitter(path, uid)
	if err != nil {
		t.Fatal(err)
	}
	return LocalAuthority{Registry: fixtureRegistry{reg}, Admitter: client, Now: now}
}

func TestLocalAuthorityRejectsUnregisteredWriterIntent(t *testing.T) {
	reg, req := authorityRegistration()
	path := filepath.Join(t.TempDir(), "absent.sock")
	a := localFixtureAuthority(t, path, uint32(os.Geteuid()), reg, nil)
	for name, change := range map[string]func(*Request){
		"double hashed target": func(r *Request) {
			r.TargetDigest = hostbridge.TargetDigest(reg.Provider, reg.ProviderOperation, r.TargetDigest)
		},
		"other plan":     func(r *Request) { r.PlanDigest = strings.Repeat("c", 64) },
		"other grant":    func(r *Request) { r.GrantDigest = strings.Repeat("c", 64) },
		"other session":  func(r *Request) { r.Session = "other" },
		"other mutation": func(r *Request) { r.MutationID = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			altered := req
			change(&altered)
			if bound, err := a.Bind(context.Background(), altered); bound != nil || !errors.Is(err, hostbridge.ErrBindingMismatch) {
				t.Fatalf("unexpected result: %v, %v", bound, err)
			}
		})
	}
}

type fixtureWireRequest struct {
	Version   int    `json:"v"`
	RequestID string `json:"request_id"`
	Method    string `json:"method"`
	hostbridge.AuthorityBinding
}

func runLocalAuthorityFixture(listener *net.UnixListener, want hostbridge.AuthorityBinding, mode string) error {
	return runLocalAuthorityFixtureAt(listener, want, mode, testNow)
}

func runLocalAuthorityFixtureAt(listener *net.UnixListener, want hostbridge.AuthorityBinding, mode string, now time.Time) error {
	var firstID string
	methods := []string{"preview_admission", "preview_execution"}
	if mode == "guarded" {
		methods[1] = "admit"
	}
	for i, method := range methods {
		if err := listener.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			return err
		}
		conn, err := listener.AcceptUnix()
		if err != nil {
			return err
		}
		err = func() error {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			raw, err := io.ReadAll(conn)
			if err != nil {
				return err
			}
			var req fixtureWireRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				return err
			}
			if req.Method != method || req.Version != 1 || req.AuthorityBinding != want || len(req.RequestID) != 32 || req.RequestID == firstID {
				return errors.New("wire method, nonce or exact registration binding differs")
			}
			firstID = req.RequestID
			bindings, _ := json.Marshal(req.AuthorityBinding)
			if method == "admit" {
				digest := sha256.Sum256(bindings)
				_, err = fmt.Fprintf(conn, `{"v":1,"request_id":%q,"status":"OK","reason":"ADMITTED",%s,"expires_at":null,"mutation_state":"RUNNING","authorizes_execution":false,"admission_digest":%q,"lease_expires_at":%q,"request_digest":%q}`,
					req.RequestID, string(bindings[1:len(bindings)-1]), want.PlanDigest, now.Add(time.Minute).Format(time.RFC3339), hex.EncodeToString(digest[:]))
				return err
			}
			status, reason := "BLOCKED", "GRANT_REVOKED"
			expiry, state := "null", "null"
			switch {
			case i == 0:
				status, reason = "ADVISORY", "EXECUTION_FENCE_UNAVAILABLE"
				expiry = `"` + now.Add(time.Minute).Format(time.RFC3339) + `"`
				if mode == "advisory expires" {
					expiry = `"` + now.Format(time.RFC3339) + `"`
				}
			case mode == "execution preview":
				status, reason = "ADVISORY", "EXECUTION_FENCE_UNAVAILABLE"
				expiry = `"` + now.Add(time.Minute).Format(time.RFC3339) + `"`
			case mode == "socket drops":
				return nil
			}
			body := fmt.Sprintf(`{"v":1,"request_id":%q,"status":%q,"reason":%q,%s,"expires_at":%s,"mutation_state":%s,"authorizes_execution":false}`,
				req.RequestID, status, reason, string(bindings[1:len(bindings)-1]), expiry, state)
			digest := "null"
			if status == "ADVISORY" {
				sum := sha256.Sum256([]byte(body))
				digest = `"` + hex.EncodeToString(sum[:]) + `"`
			}
			_, err = fmt.Fprintf(conn, `%s,"admission_digest":%s}`, body[:len(body)-1], digest)
			return err
		}()
		if err != nil {
			return err
		}
		if i == 0 && (mode == "advisory expires" || mode == "admission only") {
			return nil
		}
	}
	return nil
}

func TestGatewayLocalAuthorityNeverAuthorizesV1(t *testing.T) {
	for _, mode := range []string{"revoke after advisory", "execution preview", "socket drops", "advisory expires"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			reg, req := authorityRegistration()
			listener, path := authoritySocket(t)
			h.gw.Authority = localFixtureAuthority(t, path, uint32(os.Geteuid()), reg, h.gw.Now)
			want := hostbridge.AuthorityBinding{
				GoalID: req.GoalID, MutationID: req.MutationID, WriterGeneration: req.Generation,
				Owner: req.Owner, Session: req.Session, Provider: req.Provider, Operation: reg.Identity.OperationID,
				ResourceDigest: reg.ResourceDigest, TargetDigest: req.TargetDigest, PlanDigest: req.PlanDigest, GrantDigest: req.GrantDigest,
			}
			done := make(chan error, 1)
			go func() { done <- runLocalAuthorityFixture(listener, want, mode) }()
			receipt, err := h.gw.Submit(context.Background(), Sign(req, []byte(testKey)))
			if fixtureErr := <-done; fixtureErr != nil {
				t.Fatal(fixtureErr)
			}
			if receipt.Status != StatusNoEffect || receipt.ProviderCalls != 0 || h.provider.calls != 0 {
				t.Fatalf("provider was not blocked: %+v, %v", receipt, err)
			}
			if mode == "advisory expires" {
				if !errors.Is(err, ErrAdmissionRejected) {
					t.Fatalf("expiry: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			codes := map[string]string{
				"socket drops": "AUTHORITY_UNAVAILABLE", "revoke after advisory": "EXECUTION_FENCE_REJECTED",
				"execution preview": "EXECUTION_FENCE_UNAVAILABLE",
			}
			if receipt.Note != codes[mode] {
				t.Fatalf("unexpected fixed code: %v", receipt.Note)
			}
			record, err := h.gw.Journal.Status(req.MutationID)
			if err != nil || record.State != journal.ObservedNoEffect ||
				record.Version != 2 || record.Binding == nil || *record.Binding != want {
				t.Fatalf("journal not settled: %+v, %v", record, err)
			}
		})
	}
}

func TestGatewayLocalAuthorityReplayRequiresExactBinding(t *testing.T) {
	h := newHarness(t)
	reg, req := authorityRegistration()
	listener, path := authoritySocket(t)
	submit := func(reg RegisteredOperation, mode string) (Receipt, error) {
		h.gw.Authority = localFixtureAuthority(t, path, uint32(os.Geteuid()), reg, h.gw.Now)
		done := make(chan error, 1)
		go func() { done <- runLocalAuthorityFixture(listener, reg.authorityBinding(), mode) }()
		receipt, err := h.gw.Submit(context.Background(), Sign(req, []byte(testKey)))
		if fixtureErr := <-done; fixtureErr != nil {
			t.Fatal(fixtureErr)
		}
		return receipt, err
	}
	first, err := submit(reg, "execution preview")
	if err != nil || first.Status != StatusNoEffect || first.ProviderCalls != 0 {
		t.Fatalf("first dispatch: %+v, %v", first, err)
	}
	replay, err := submit(reg, "admission only")
	if err != nil || replay.Status != StatusDuplicate || replay.ProviderCalls != 0 {
		t.Fatalf("exact replay: %+v, %v", replay, err)
	}
	changed := reg
	changed.ResourceDigest = strings.Repeat("f", 64)
	if receipt, err := submit(changed, "admission only"); !errors.Is(err, ErrMutationConflict) || receipt.ProviderCalls != 0 {
		t.Fatalf("changed registration reused mutation ID: %+v, %v", receipt, err)
	}
	record, err := h.gw.Journal.Status(req.MutationID)
	if err != nil || record.Binding == nil || *record.Binding != reg.authorityBinding() || h.provider.calls != 0 {
		t.Fatalf("replay changed the original binding or called provider: %+v, %v", record, err)
	}
}

func TestGatewayLocalAuthorityUnavailable(t *testing.T) {
	h := newHarness(t)
	reg, req := authorityRegistration()
	h.gw.Authority = localFixtureAuthority(t, filepath.Join(t.TempDir(), "missing.sock"), uint32(os.Geteuid()), reg, h.gw.Now)
	receipt, err := h.gw.Submit(context.Background(), Sign(req, []byte(testKey)))
	if !errors.Is(err, ErrAuthorityUnavail) || receipt.Status != StatusNoEffect || h.provider.calls != 0 {
		t.Fatalf("socket outage: %+v, %v", receipt, err)
	}
}
