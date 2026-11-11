//go:build darwin || linux

package hostbridge

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSettlementWireV1(t *testing.T) {
	req := authorityRequest{1, "request-1", "finish", reservationFixtureBinding()}
	reply := reservationFixtureReply(req)
	reply.Reason, reply.AdmissionDigest = "FINISHED", nil
	for _, state := range []string{"DONE", "FAILED"} {
		reply.MutationState = ptr(state)
		raw, _ := json.Marshal(reply)
		if _, err := validateAuthorityReply(req, raw); err != nil {
			t.Fatalf("Python-compatible %s settlement rejected: %v", state, err)
		}
		for _, method := range []string{"admit", "preview_execution", "preview_admission"} {
			other := req
			other.Method = method
			if _, err := validateAuthorityReply(other, raw); err != ErrLocalAuthorityInvalid {
				t.Fatalf("%s accepted a finish receipt: %v", method, err)
			}
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*authorityReply)
	}{
		{"different binding", func(r *authorityReply) { r.GoalID = "other" }},
		{"different nonce", func(r *authorityReply) { r.RequestID = "other" }},
		{"different request digest", func(r *authorityReply) { r.RequestDigest = ptr(strings.Repeat("a", 64)) }},
		{"missing request digest", func(r *authorityReply) { r.RequestDigest = nil }},
		{"admission digest", func(r *authorityReply) { r.AdmissionDigest = ptr(strings.Repeat("a", 64)) }},
		{"advisory expiry", func(r *authorityReply) { r.ExpiresAt = r.LeaseExpiresAt }},
		{"invalid lease expiry", func(r *authorityReply) { r.LeaseExpiresAt = ptr("2026-11-11T12:01:00+00:00") }},
		{"missing lease expiry", func(r *authorityReply) { r.LeaseExpiresAt = nil }},
		{"nonterminal state", func(r *authorityReply) { r.MutationState = ptr("RUNNING") }},
		{"wrong reason", func(r *authorityReply) { r.Reason = "ADMITTED" }},
		{"wrong status", func(r *authorityReply) { r.Status = "BLOCKED" }},
		{"execution authority", func(r *authorityReply) { r.Authorizes = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := reply
			tc.change(&bad)
			raw, _ := json.Marshal(bad)
			if _, err := validateAuthorityReply(req, raw); err != ErrLocalAuthorityInvalid {
				t.Fatalf("invalid settlement receipt accepted: %v", err)
			}
		})
	}
}

func TestFinishUnix(t *testing.T) {
	root, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(root, "kc-finish-")
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
	if _, err := client.Finish(context.Background(), binding); !errors.Is(err, ErrLocalAuthorityUnavailable) || errors.Is(err, ErrSettlementUnknown) {
		t.Fatalf("pre-send failure must be unavailable: %v", err)
	}

	for _, tc := range []struct {
		mode string
		want error
	}{
		{"DONE", nil},
		{"FAILED", nil},
		{"UNRESOLVED_EFFECT", ErrAdmissionRejected},
		{"JOURNAL_DB_MISMATCH", ErrSettlementUnknown},
		{"NOT_RUNNING", ErrSettlementUnknown},
		{"AUTHORITY_UNAVAILABLE", ErrSettlementUnknown},
		{"LOST", ErrSettlementUnknown},
		{"INVALID", ErrSettlementUnknown},
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
			go func() { done <- serveAuthorityMutationFixture(listener, binding, "finish", tc.mode) }()
			result, err := client.Finish(context.Background(), binding)
			if serverErr := <-done; serverErr != nil {
				t.Fatal(serverErr)
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Finish returned %v, want %v", err, tc.want)
			}
			if tc.want != nil {
				if result != (MutationSettlement{}) {
					t.Fatalf("failed settlement returned a receipt: %+v", result)
				}
				return
			}
			if result.State != tc.mode || result.RequestDigest != reservationFixtureDigest ||
				!result.LeaseExpiresAt.Equal(time.Date(2026, 11, 11, 12, 1, 0, 123456000, time.UTC)) {
				t.Fatalf("unexpected receipt: %+v", result)
			}
		})
	}
}
