//go:build darwin || linux

package hostbridge

import (
	"context"
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
)

// Generated with workspace-infra tooling/keycli_gateway_authority.py:intent_digest.
const reservationFixtureDigest = "eff122346c1acd0e62a5ed55fa623d9f7876d03465b0e09fe547802175498687"

func reservationFixtureBinding() AuthorityBinding {
	return AuthorityBinding{
		GoalID: "goal-1", MutationID: "mutation-1", WriterGeneration: 2,
		Owner: "writer", Session: "session-1", Provider: "cloudflare", Operation: "operation-1",
		ResourceDigest: strings.Repeat("1", 64), TargetDigest: strings.Repeat("2", 64),
		PlanDigest: strings.Repeat("3", 64), GrantDigest: strings.Repeat("4", 64),
	}
}

func reservationFixtureReply(req authorityRequest) authorityReply {
	return authorityReply{
		authorityAdvisory: authorityAdvisory{
			Version: 1, RequestID: req.RequestID, Status: "OK", Reason: "ADMITTED",
			AuthorityBinding: req.AuthorityBinding, MutationState: ptr("RUNNING"),
		},
		AdmissionDigest: ptr(strings.Repeat("a", 64)),
		LeaseExpiresAt:  ptr("2026-11-11T12:01:00.123456Z"),
		RequestDigest:   ptr(reservationFixtureDigest),
	}
}

func TestReservationWireV1(t *testing.T) {
	req := authorityRequest{1, "request-1", "admit", reservationFixtureBinding()}
	admitted := reservationFixtureReply(req)
	raw, err := json.Marshal(admitted)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validateAuthorityReply(req, raw); err != nil {
		t.Fatalf("Python-compatible admission receipt rejected: %v", err)
	}
	for _, state := range []string{"RUNNING", "DONE", "FAILED", "ORPHANED", "RECOVERED"} {
		t.Run("duplicate "+state, func(t *testing.T) {
			dup := admitted
			dup.Reason, dup.MutationState, dup.AdmissionDigest = "DUPLICATE", ptr(state), nil
			raw, _ := json.Marshal(dup)
			if _, err := validateAuthorityReply(req, raw); err != nil {
				t.Fatalf("exact duplicate rejected: %v", err)
			}
		})
	}

	for _, tc := range []struct {
		name   string
		change func(*authorityReply)
	}{
		{"different nonce", func(r *authorityReply) { r.RequestID = "another" }},
		{"different intent", func(r *authorityReply) { r.Operation = "another" }},
		{"different generation", func(r *authorityReply) { r.WriterGeneration++ }},
		{"changed intent digest", func(r *authorityReply) { r.RequestDigest = ptr(strings.Repeat("b", 64)) }},
		{"invalid admission digest", func(r *authorityReply) { r.AdmissionDigest = ptr(strings.Repeat("A", 64)) }},
		{"missing admission digest", func(r *authorityReply) { r.AdmissionDigest = nil }},
		{"missing lease expiry", func(r *authorityReply) { r.LeaseExpiresAt = nil }},
		{"invalid lease expiry", func(r *authorityReply) { r.LeaseExpiresAt = ptr("2026-02-30T12:01:00Z") }},
		{"advisory expiry in receipt", func(r *authorityReply) { r.ExpiresAt = r.LeaseExpiresAt }},
		{"unexpected status", func(r *authorityReply) { r.Status = "BLOCKED" }},
		{"unexpected reason", func(r *authorityReply) { r.Reason = "FINISHED" }},
		{"unexpected state", func(r *authorityReply) { r.MutationState = ptr("DONE") }},
		{"duplicate with admission digest", func(r *authorityReply) { r.Reason = "DUPLICATE" }},
		{"duplicate with unsupported state", func(r *authorityReply) {
			r.Reason, r.MutationState, r.AdmissionDigest = "DUPLICATE", ptr("UNBOUND"), nil
		}},
		{"unexpected authority", func(r *authorityReply) { r.Authorizes = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := admitted
			tc.change(&bad)
			raw, _ := json.Marshal(bad)
			if _, err := validateAuthorityReply(req, raw); err != ErrLocalAuthorityInvalid {
				t.Fatalf("invalid reservation receipt accepted: %v", err)
			}
		})
	}
	for _, bad := range []string{
		`{"request_digest":"other",` + string(raw[1:]),
		strings.Replace(string(raw), `"lease_expires_at":"2026-11-11T12:01:00.123456Z"`, `"lease_expires_at":null`, 1),
		`{"extra":"unexpected",` + string(raw[1:]),
	} {
		if _, err := validateAuthorityReply(req, []byte(bad)); err != ErrLocalAuthorityInvalid {
			t.Fatalf("invalid reservation frame accepted: %v", err)
		}
	}
	for _, method := range []string{"preview_admission", "preview_execution"} {
		preview := req
		preview.Method = method
		if _, err := validateAuthorityReply(preview, raw); err != ErrLocalAuthorityInvalid {
			t.Fatalf("%s accepted an admission receipt: %v", method, err)
		}
	}
}

func TestReserveUnix(t *testing.T) {
	root, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "kc-res-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "authority.sock")
	client, err := NewLocalAdmitter(path, uint32(os.Geteuid()))
	if err != nil {
		t.Fatal(err)
	}
	binding := reservationFixtureBinding()
	if _, err := client.Reserve(context.Background(), binding); !errors.Is(err, ErrLocalAuthorityUnavailable) || errors.Is(err, ErrReservationUnknown) {
		t.Fatalf("pre-send failure must be unavailable: %v", err)
	}

	for _, tc := range []struct {
		mode string
		want error
	}{
		{"ADMITTED", nil},
		{"DUPLICATE", nil},
		{"GRANT_CHANGED", ErrAdmissionRejected},
		{"BUDGET_UNVERIFIED", ErrReservationUnknown},
		{"AUTHORITY_UNAVAILABLE", ErrReservationUnknown},
		{"ADVISORY", ErrExecutionFenceUnavailable},
		{"LOST", ErrReservationUnknown},
		{"INVALID", ErrReservationUnknown},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := os.Chmod(path, 0600); err != nil {
				t.Fatal(err)
			}
			if err := listener.SetDeadline(time.Now().Add(localDeadline)); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				done <- serveAuthorityMutationFixture(listener, binding, "admit", tc.mode)
			}()

			result, err := client.Reserve(context.Background(), binding)
			if serverErr := <-done; serverErr != nil {
				t.Fatal(serverErr)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Reserve returned %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if result != (MutationReservation{}) {
					t.Fatalf("failed reservation returned a receipt: %+v", result)
				}
				return
			}
			wantState, wantDigest := "RUNNING", strings.Repeat("a", 64)
			if tc.mode == "DUPLICATE" {
				wantState, wantDigest = "DONE", ""
			}
			if result.Reason != tc.mode || result.State != wantState ||
				result.RequestDigest != reservationFixtureDigest || result.AdmissionDigest != wantDigest ||
				!result.LeaseExpiresAt.Equal(time.Date(2026, 11, 11, 12, 1, 0, 123456000, time.UTC)) {
				t.Fatalf("unexpected receipt: %+v", result)
			}
		})
	}
}

func serveAuthorityMutationFixture(listener *net.UnixListener, binding AuthorityBinding, method, mode string) error {
	conn, err := listener.AcceptUnix()
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(localDeadline)); err != nil {
		return err
	}
	payload, err := io.ReadAll(conn)
	if err != nil {
		return err
	}
	var req authorityRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return err
	}
	canonical, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if string(canonical) != string(payload) || req.Version != 1 || req.Method != method || req.AuthorityBinding != binding || len(req.RequestID) != 32 {
		return fmt.Errorf("unexpected Authority mutation request")
	}
	reply := reservationFixtureReply(req)
	if method == "finish" {
		reply.Reason, reply.AdmissionDigest = "FINISHED", nil
		reply.MutationState = ptr("DONE")
	}
	switch mode {
	case "LOST":
		return nil
	case "INVALID":
		reply.RequestID = "mismatched"
	case "DUPLICATE":
		reply.Reason, reply.MutationState, reply.AdmissionDigest = "DUPLICATE", ptr("DONE"), nil
	case "FAILED":
		reply.MutationState = ptr("FAILED")
	case "GRANT_CHANGED", "BUDGET_UNVERIFIED", "AUTHORITY_UNAVAILABLE", "UNRESOLVED_EFFECT", "JOURNAL_DB_MISMATCH", "NOT_RUNNING":
		reply = authorityReply{authorityAdvisory: authorityAdvisory{
			Version: 1, RequestID: req.RequestID, Status: "BLOCKED", Reason: mode, AuthorityBinding: binding,
		}}
	case "ADVISORY":
		reply = authorityReply{authorityAdvisory: authorityAdvisory{
			Version: 1, RequestID: req.RequestID, Status: "ADVISORY", Reason: "EXECUTION_FENCE_UNAVAILABLE",
			AuthorityBinding: binding, ExpiresAt: ptr("2026-11-11T12:01:00Z"),
		}}
		reply.AdmissionDigest = ptr(jsonDigest(reply.authorityAdvisory))
	}
	return json.NewEncoder(conn).Encode(reply)
}
