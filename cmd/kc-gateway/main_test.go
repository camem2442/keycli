//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
)

// Re-execute the test binary to exercise main, including flags, startup,
// session issuance and HTTP wiring, without building a second Go binary.
func TestGatewayProcess(t *testing.T) {
	args := os.Args
	i := slices.Index(args, "--")
	if i < 0 {
		return
	}
	flag.CommandLine = flag.NewFlagSet("kc-gateway", flag.ExitOnError)
	os.Args = append([]string{args[0]}, args[i+1:]...)
	main()
}

func TestGatewayStartupWithHostAuthority(t *testing.T) {
	sqlite, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("requires the gateway's sqlite3 runtime dependency")
	}
	dir := gatewayTempDir(t)
	socket := filepath.Join(dir, "a.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	want := hostbridge.AuthorityBinding{
		GoalID: "test-goal", MutationID: "test-mutation", WriterGeneration: 7,
		Owner: "test-owner", Session: "test-session", Provider: "cloudflare",
		Operation: "registered-op", ResourceDigest: strings.Repeat("a", 64),
		PlanDigest: strings.Repeat("b", 64), GrantDigest: strings.Repeat("c", 64),
		TargetDigest: hostbridge.TargetDigest("cloudflare", "create-account-token", "fixture/account"),
	}
	config := filepath.Join(dir, "authority.json")
	reg := fmt.Sprintf(`{"goal_id":%q,"mutation_id":%q,"operation":%q,"writer_generation":7,
		"owner":%q,"session":%q,"provider":"cloudflare","provider_operation":"create-account-token",
		"canonical_target":"fixture/account","resource_digest":%q,"plan_digest":%q,"grant_digest":%q}`,
		want.GoalID, want.MutationID, want.Operation, want.Owner, want.Session, want.ResourceDigest, want.PlanDigest, want.GrantDigest)
	raw := []byte(fmt.Sprintf(`{"v":1,"socket_path":%q,"socket_uid":%d,"registrations":[%s]}`, socket, os.Geteuid(), reg))
	if err := os.WriteFile(config, raw, 0600); err != nil {
		t.Fatal(err)
	}

	db := filepath.Join(dir, "claims.db")
	if out, err := exec.Command(sqlite, db, `CREATE TABLE claims(goal_id,generation,state,owner,owner_session);
		INSERT INTO claims VALUES('test-goal',7,'ACTIVE','test-owner','test-session');`).CombinedOutput(); err != nil {
		t.Fatalf("fixture sqlite: %v (%s)", err, out)
	}
	done := make(chan error, 1)
	runGatewaySubmit(t, dir, db, config, want, "EXECUTION_FENCE_REJECTED", func() {
		go func() { done <- denyAtExecution(listener, want) }()
	})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func gatewayTempDir(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(base, "kc-gw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func runGatewaySubmit(t *testing.T, dir, db, config string, want hostbridge.AuthorityBinding, wantNote string, beforeSubmit func()) {
	t.Helper()
	runGatewayProcessSubmit(t, dir, db, config, want, effectgateway.Receipt{
		Status: effectgateway.StatusNoEffect, OperationState: string(journal.ObservedNoEffect), Note: wantNote,
	}, false, func(_ int) {
		if beforeSubmit != nil {
			beforeSubmit()
		}
	})
}

func runGatewayProcessSubmit(t *testing.T, dir, db, config string, want hostbridge.AuthorityBinding, expected effectgateway.Receipt, retryReservation bool, beforeSubmit func(int)) {
	t.Helper()
	address, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := address.Addr().String()
	_ = address.Close()
	keyPath, ledgerPath, journalPath := filepath.Join(dir, "key"), filepath.Join(dir, "ledger"), filepath.Join(dir, "journal")
	args := []string{"-test.run=^TestGatewayProcess$", "--", "-provider", "synthetic", "-addr", addr,
		"-journal", journalPath, "-claims", db, "-key-out", keyPath, "-ledger", ledgerPath,
		"-goal", want.GoalID, "-owner", want.Owner, "-session", want.Session, "-generation", strconv.FormatInt(want.WriterGeneration, 10)}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Explicitly malformed configuration must stop before key issuance.
	bad := exec.CommandContext(ctx, os.Args[0], append(slices.Clone(args), "-authority-config=")...)
	if out, err := bad.CombinedOutput(); err == nil || !bytes.Contains(out, []byte("invalid or unsafe host authority configuration")) {
		t.Fatalf("bad startup: %v (%s)", err, out)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("bad startup wrote a session key: %v", err)
	}

	cmd := exec.CommandContext(ctx, os.Args[0], append(slices.Clone(args), "-authority-config", config)...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	client := &http.Client{Timeout: 6 * time.Second}
	url := "http://" + addr
	for {
		res, err := client.Get(url + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("configured gateway did not become healthy")
		case <-time.After(20 * time.Millisecond):
		}
	}
	keyText, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(keyText)))
	if err != nil {
		t.Fatal("invalid issued key format")
	}
	if beforeSubmit != nil {
		beforeSubmit(cmd.Process.Pid)
	}
	req := effectgateway.Request{
		GoalID: want.GoalID, MutationID: want.MutationID, Generation: want.WriterGeneration,
		Owner: want.Owner, Session: want.Session, Provider: want.Provider, Action: "issue",
		TargetDigest: want.TargetDigest, PlanDigest: want.PlanDigest, GrantDigest: want.GrantDigest,
		ExpiresAt: time.Now().Add(time.Minute),
	}
	payload, err := json.Marshal(effectgateway.Sign(req, key))
	if err != nil {
		t.Fatal(err)
	}
	post := func() *http.Response {
		res, err := client.Post(url+"/submit", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	if retryReservation {
		res := post()
		var response map[string]string
		decodeErr := json.NewDecoder(res.Body).Decode(&response)
		res.Body.Close()
		if res.StatusCode != http.StatusServiceUnavailable || decodeErr != nil || response["error"] != "RESERVATION_UNKNOWN" {
			t.Fatalf("expected unknown reservation: HTTP %d, %v, %v", res.StatusCode, response, decodeErr)
		}
		if _, err := (journal.Store{Root: journalPath}).Status(want.MutationID); !errors.Is(err, journal.ErrNotFound) {
			t.Fatalf("unknown reservation created a Go dispatch record: %v", err)
		}
	}
	res := post()
	defer res.Body.Close()
	var receipt effectgateway.Receipt
	if res.StatusCode != http.StatusOK || json.NewDecoder(res.Body).Decode(&receipt) != nil ||
		receipt.Status != expected.Status || receipt.ProviderCalls != expected.ProviderCalls ||
		receipt.AuthoritySettlement != expected.AuthoritySettlement ||
		receipt.OperationState != expected.OperationState || (expected.Note != "" && receipt.Note != expected.Note) {
		t.Fatalf("unexpected submit: HTTP %d, %+v", res.StatusCode, receipt)
	}
	if expected.ProviderCalls == 0 {
		if _, err := os.Stat(ledgerPath); !os.IsNotExist(err) {
			t.Fatalf("provider ledger should not exist: %v", err)
		}
	} else {
		raw, err := os.ReadFile(ledgerPath)
		var entry ledgerEntry
		if err != nil || json.Unmarshal(raw, &entry) != nil || entry.Seq != 1 || entry.Mutation != want.MutationID {
			t.Fatalf("expected one provider ledger entry for the bound mutation: %v", err)
		}
	}
	record, err := (journal.Store{Root: journalPath}).Status(want.MutationID)
	if expected.OperationState == "" && errors.Is(err, journal.ErrNotFound) {
		return
	}
	if err != nil || record.Version != 2 || string(record.State) != expected.OperationState ||
		record.Binding == nil || *record.Binding != want {
		t.Fatalf("unexpected persisted Authority binding: %+v, %v", record, err)
	}
}

func denyAtExecution(listener *net.UnixListener, want hostbridge.AuthorityBinding) error {
	var priorID string
	for _, method := range []string{"preview_admission", "preview_execution"} {
		_ = listener.SetDeadline(time.Now().Add(3 * time.Second))
		conn, err := listener.AcceptUnix()
		if err != nil {
			return err
		}
		err = func() error {
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			raw, err := io.ReadAll(io.LimitReader(conn, 64<<10))
			if err != nil {
				return err
			}
			var wire struct {
				Version   int    `json:"v"`
				Method    string `json:"method"`
				RequestID string `json:"request_id"`
				hostbridge.AuthorityBinding
			}
			if json.Unmarshal(raw, &wire) != nil || wire.Version != 1 || wire.Method != method ||
				wire.AuthorityBinding != want || len(wire.RequestID) != 32 || wire.RequestID == priorID {
				return fmt.Errorf("socket received a different request than the bound %s fixture", method)
			}
			priorID = wire.RequestID
			binding, _ := json.Marshal(want)
			status, reason, expiry := "BLOCKED", "GRANT_REVOKED", "null"
			if method == "preview_admission" {
				status, reason = "ADVISORY", "EXECUTION_FENCE_UNAVAILABLE"
				expiry = fmt.Sprintf("%q", time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano))
			}
			body := fmt.Sprintf(`{"v":1,"request_id":%q,"status":%q,"reason":%q,%s,"expires_at":%s,"mutation_state":null,"authorizes_execution":false}`,
				wire.RequestID, status, reason, binding[1:len(binding)-1], expiry)
			digest := "null"
			if method == "preview_admission" {
				digest = fmt.Sprintf("\"%x\"", sha256.Sum256([]byte(body)))
			}
			_, err = fmt.Fprintf(conn, `%s,"admission_digest":%s}`, body[:len(body)-1], digest)
			return err
		}()
		if err != nil {
			return err
		}
	}
	return nil
}
