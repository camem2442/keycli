//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
)

func TestInstalledOperationalGateway(t *testing.T) {
	dir := gatewayTempDir(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	build := func(name, channel string) string {
		path := filepath.Join(dir, name)
		link := "-X github.com/camem2442/keycli/internal/effectgateway.channel=" + channel
		if runtime.GOOS == "darwin" && strings.HasPrefix(runtime.Version(), "go1.22") {
			// Go 1.22's internal linker omits the LC_UUID required by newer macOS.
			link += " -linkmode=external"
		}
		if out, err := exec.CommandContext(ctx, "go", "build", "-ldflags", link, "-o", path, ".").CombinedOutput(); err != nil {
			t.Fatalf("build gateway: %v (%s)", err, out)
		}
		return path
	}
	live := build("installed-live", "operational")
	dev := build("installed-dev", "dev")
	env := func(enable string) []string {
		result := make([]string, 0, len(os.Environ())+1)
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, effectgateway.ProviderEffectsEnv+"=") {
				result = append(result, value)
			}
		}
		return append(result, effectgateway.ProviderEffectsEnv+"="+enable)
	}
	fail := func(binary, runtime, expected string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = env(runtime)
		out, err := cmd.CombinedOutput()
		if err == nil || !bytes.Contains(out, []byte(expected)) || bytes.Contains(out, []byte(dir)) {
			t.Fatalf("unexpected startup result: %v (%s)", err, out)
		}
	}
	fail(dev, "enabled", "provider effects are locked", "-provider", "live")
	fail(live, "", "provider effects are locked", "-provider", "live")
	fail(live, "enabled", "requires Authority, product and read grant configuration", "-provider", "live")
	fail(live, "enabled", "synthetic options are unavailable", "-provider", "live", "-ledger", "")
	fail(live, "enabled", "select -provider live or -provider synthetic explicitly")
	for _, address := range []string{"0.0.0.0:0", "localhost:0", "[::]:0", "127.0.0.1:http", "127.0.0.1:65536", "[::1%lo0]:0"} {
		fail(live, "enabled", "must be a literal loopback", "-provider", "synthetic", "-addr", address)
	}
	if _, err := os.Stat("/usr/bin/sqlite3"); err != nil {
		t.Skip("startup fixture requires /usr/bin/sqlite3")
	}
	write := func(path string, body any) {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	socket := func(name string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		if err := os.Chmod(path, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	now := time.Now().UTC().Truncate(time.Second)
	account, group := strings.Repeat("a", 32), strings.Repeat("b", 32)
	name := "kc-rot-fixture"
	canonical := account + "/tokens/" + name + "/permission-group/" + group
	old := rotation.Generation{Alias: "fixture", Kind: rotation.StaticServiceKey, Provider: "cloudflare", Target: account, Number: 3}
	next := old
	next.Number = 4
	plan, err := rotation.NewPreparationPlan(old, next, []rotation.Consumer{{ID: "fixture-consumer", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	binding := authoritybinding.Binding{
		GoalID: "test-goal", MutationID: "test-mutation", WriterGeneration: 7, Owner: "test-owner",
		Session: "test-session", Provider: "cloudflare", Operation: "registered-op",
		ResourceDigest: strings.Repeat("a", 64), GrantDigest: strings.Repeat("b", 64), PlanDigest: plan.ID,
		TargetDigest: hostbridge.TargetDigest("cloudflare", "create-account-token", canonical),
	}
	authority := filepath.Join(dir, "authority.json")
	admitter, guarded := socket("admit.sock"), socket("execute.sock")
	registration := map[string]any{
		"goal_id": binding.GoalID, "mutation_id": binding.MutationID, "operation": binding.Operation, "writer_generation": binding.WriterGeneration,
		"owner": binding.Owner, "session": binding.Session, "provider": binding.Provider, "provider_operation": "create-account-token",
		"canonical_target": canonical, "resource_digest": binding.ResourceDigest, "plan_digest": binding.PlanDigest, "grant_digest": binding.GrantDigest,
	}
	authorityData := map[string]any{"v": 1, "socket_path": admitter, "socket_uid": os.Geteuid(), "registrations": []any{registration}}
	write(authority, authorityData)
	issuer, reader, sink := filepath.Join(dir, "issuer"), filepath.Join(dir, "reader"), filepath.Join(dir, "issued")
	if err := os.WriteFile(issuer, []byte("synthetic-issuer-fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reader, []byte("synthetic-reader-fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sink, 0700); err != nil {
		t.Fatal(err)
	}
	product := filepath.Join(dir, "product.json")
	productOp := map[string]any{
		"goal_id": binding.GoalID, "mutation_id": binding.MutationID, "account_id": account, "token_name": name,
		"permission_group_id": group, "token_expires_at": now.Add(time.Hour), "grant_expires_at": now.Add(30 * time.Minute),
		"credential_alias": old.Alias, "old_generation": old.Number, "new_generation": next.Number, "required_consumers": []string{"fixture-consumer"},
	}
	productData := map[string]any{"v": 1, "issuer_file": issuer, "reader_file": reader, "secret_store": sink, "operations": []any{productOp}}
	write(product, productData)
	read := filepath.Join(dir, "reads.json")
	write(read, map[string]any{"v": 1, "grants": []any{
		map[string]any{
			"binding": binding, "owner": binding.Owner, "session": binding.Session, "generation": binding.WriterGeneration,
			"action": effectgateway.ReadStatus, "grant_digest": strings.Repeat("c", 64),
			"provider_identity_digest": strings.Repeat("d", 64), "expires_at": now.Add(time.Hour),
		},
	}})
	db := filepath.Join(dir, "claims.db")
	sql := `CREATE TABLE claims(goal_id,generation,state,owner,owner_session);
	INSERT INTO claims VALUES('test-goal',7,'ACTIVE','test-owner','test-session');`
	if out, err := exec.Command("/usr/bin/sqlite3", db, sql).CombinedOutput(); err != nil {
		t.Fatalf("fixture sqlite: %v (%s)", err, out)
	}
	if err := os.Chmod(db, 0600); err != nil {
		t.Fatal(err)
	}
	journalPath, keyPath := filepath.Join(dir, "journal"), filepath.Join(dir, "key")
	address, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := address.Addr().String()
	_ = address.Close()
	args := []string{"-provider", "live", "-addr", addr, "-authority-config", authority, "-product-config", product,
		"-read-config", read, "-journal", journalPath, "-claims", db, "-key-out", keyPath,
		"-goal", binding.GoalID, "-owner", binding.Owner, "-session", binding.Session, "-generation", "7"}
	fail(live, "enabled", "requires protected Authority configuration and guarded execution", args...)
	authorityData["guarded_execution"] = map[string]any{"socket_path": filepath.Join(dir, "missing.sock"), "socket_uid": os.Geteuid(), "socket_pid": os.Getpid()}
	write(authority, authorityData)
	fail(live, "enabled", "requires protected Authority configuration and guarded execution", args...)
	authorityData["guarded_execution"] = map[string]any{"socket_path": guarded, "socket_uid": os.Geteuid(), "socket_pid": os.Getpid()}
	write(authority, authorityData)

	productOp["token_name"] = "kc-rot-wrong"
	write(product, productData)
	fail(live, "enabled", "invalid or unsafe operational provider configuration", args...)
	productOp["token_name"] = name
	write(product, productData)
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("invalid startup issued key: %v", err)
	}

	store := journal.Store{Root: journalPath}
	intent := journal.Intent{OperationID: binding.MutationID, TargetDigest: binding.TargetDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest}
	if _, err := store.DispatchBound(intent, binding, func(journal.DispatchGuard) (journal.Observation, error) {
		return journal.Observation{State: journal.Acknowledged, ProofDigest: strings.Repeat("e", 64)}, nil
	}); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{}}
	defer client.CloseIdleConnections()
	base := "http://" + addr
	start := func(args []string) func() {
		t.Helper()
		cmd := exec.CommandContext(ctx, live, args...)
		cmd.Env = env("enabled")
		var output bytes.Buffer
		cmd.Stdout, cmd.Stderr = &output, &output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = cmd.Process.Signal(os.Interrupt)
			if err := cmd.Wait(); err != nil {
				t.Errorf("gateway exit: %v (%s)", err, output.String())
			}
			for _, secret := range []string{dir, "synthetic-issuer-fixture", "synthetic-reader-fixture"} {
				if strings.Contains(output.String(), secret) {
					t.Error("operational logs leaked private information")
				}
			}
		}
		t.Cleanup(stop)
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
			res, err := client.Get(base + "/health")
			if err != nil {
				continue
			}
			body, readErr := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK && readErr == nil && string(body) == "{\"status\":\"ok\",\"gateway\":true}\n" {
				return stop
			}
		}
		t.Fatal("installed operational gateway did not become healthy")
		return stop
	}
	post := func(endpoint string, payload any, expected int) {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Post(base+endpoint, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != expected {
			body, _ := io.ReadAll(res.Body)
			t.Fatalf("%s returned HTTP %d, want %d (%s)", endpoint, res.StatusCode, expected, body)
		}
	}

	stop := start(args)
	key, err := effectgateway.ReadSessionKeyFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	readReq := effectgateway.ReadRequest{
		GoalID: binding.GoalID, MutationID: binding.MutationID, Owner: binding.Owner, Session: binding.Session,
		Generation: binding.WriterGeneration, ExpiresAt: time.Now().Add(time.Minute), Action: effectgateway.ReadStatus,
	}
	post("/status", effectgateway.SignRead(readReq, key), http.StatusOK)
	readReq.Action = effectgateway.ReadReconcile
	post("/reconcile", effectgateway.SignRead(readReq, key), http.StatusForbidden)
	stop()

	fail(live, "enabled", "operational session issuance failed", args...)
	restart := slices.Clone(args)
	keyIndex := slices.Index(restart, "-key-out") + 1
	restart[keyIndex] = filepath.Join(dir, "key-new")
	stop = start(restart)
	readReq.Action = effectgateway.ReadStatus
	post("/status", effectgateway.SignRead(readReq, key), http.StatusUnauthorized)
	newKey, err := effectgateway.ReadSessionKeyFile(restart[keyIndex])
	if err != nil || bytes.Equal(newKey, key) {
		t.Fatal("gateway restart failed to issue a fresh key")
	}
	post("/status", effectgateway.SignRead(readReq, newKey), http.StatusOK)
	stop()
}
