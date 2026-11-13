package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectclient"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/rotation"
)

func TestRotationCommandDoesNotOpenSecretStore(t *testing.T) {
	input := writeRotationInputFixture(t, rotationInputFixture, 0600)
	root := newRoot(func(context.Context) (*app.App, error) {
		t.Fatal("local rotation command opened the secret store")
		return nil, nil
	})
	var stdout, stderr bytes.Buffer
	root.SetArgs([]string{"rotation", "plan", "--input", input})
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	if code := runRoot(root); code != 0 || stderr.Len() != 0 {
		t.Fatalf("plan exit=%d, stderr=%s", code, stderr.String())
	}
	var out rotationOutput
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil || out.Version != 1 || out.Plan == nil ||
		!journal.ValidDigest(out.Plan.PlanDigest) || out.Plan.Old.Number != 1 || out.Plan.New.Number != 2 {
		t.Fatalf("invalid plan projection: %s", stdout.String())
	}
}

func TestRotationEffectOutcomeMapping(t *testing.T) {
	const mutation = "fixture-mutation"
	for _, tc := range []struct {
		status, state, action, proof, outcome string
		want                                  int
	}{
		{effectgateway.StatusApplied, string(journal.Acknowledged), effectclient.Submit, strings.Repeat("a", 64), "ACKNOWLEDGED_UNVERIFIED", 3},
		{effectgateway.StatusApplied, string(journal.ObservedApplied), effectgateway.ReadReconcile, strings.Repeat("a", 64), "OBSERVED_APPLIED", 0},
		{effectgateway.StatusUnknown, string(journal.UnknownEffect), effectclient.Submit, "", "UNKNOWN_EFFECT", 3},
		{effectgateway.StatusUnknown, string(journal.UnknownEffect), effectgateway.ReadStatus, "", "UNKNOWN_EFFECT", 0},
		{effectgateway.StatusNoEffect, string(journal.ObservedNoEffect), effectgateway.ReadReconcile, strings.Repeat("a", 64), "OBSERVED_NO_EFFECT", 0},
		{effectgateway.StatusDuplicate, "", effectclient.Submit, "", "DUPLICATE", 3},
		{effectgateway.StatusApplied, string(journal.ObservedApplied), effectclient.Submit, "not-a-digest", "", 3},
		{effectgateway.StatusApplied, string(journal.UnknownEffect), effectclient.Submit, "", "", 3},
	} {
		effect, code := projectRotationEffect(effectclient.Result{Receipt: effectgateway.Receipt{
			MutationID: mutation, Status: tc.status, OperationState: tc.state, ProofDigest: tc.proof,
		}}, tc.action)
		if code != tc.want || (effect != nil && effect.Outcome != tc.outcome) || (effect == nil && tc.outcome != "") {
			t.Errorf("%s %s: outcome=%+v exit=%d", tc.action, tc.state, effect, code)
		}
	}
}

func TestRotationEffectPreservesAuthoritySettlement(t *testing.T) {
	for _, tc := range []struct {
		name, settlement, status string
		state                    journal.State
		want                     string
	}{
		{"finished", effectgateway.SettlementDone, effectgateway.StatusApplied, journal.Acknowledged, effectgateway.SettlementDone},
		{"no effect", effectgateway.SettlementFailed, effectgateway.StatusNoEffect, journal.ObservedNoEffect, effectgateway.SettlementFailed},
		{"finish unconfirmed", effectgateway.SettlementUnconfirmed, effectgateway.StatusApplied, journal.Acknowledged, effectgateway.SettlementUnconfirmed},
		{"legacy receipt", "", effectgateway.StatusApplied, journal.Acknowledged, ""},
		{"unrecognized text", "fixture-private-value", effectgateway.StatusApplied, journal.Acknowledged, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := effectclient.Result{Receipt: effectgateway.Receipt{
				MutationID: "fixture-mutation", Status: tc.status, OperationState: string(tc.state),
				ProofDigest: strings.Repeat("a", 64), AuthoritySettlement: tc.settlement,
			}}
			effect, code := projectRotationEffect(result, effectclient.Submit)
			result.Receipt.AuthoritySettlement = ""
			baseline, baselineCode := projectRotationEffect(result, effectclient.Submit)
			if effect == nil || baseline == nil || code != baselineCode || effect.AuthoritySettlement != tc.want {
				t.Fatalf("unexpected projection: effect=%+v exit=%d", effect, code)
			}
			raw, err := json.Marshal(effect)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" && bytes.Contains(raw, []byte("authority_settlement")) {
				t.Fatalf("optional field should be omitted: %s", raw)
			}
			effect.AuthoritySettlement = ""
			if !bytes.Equal(mustRotationEffectJSON(t, effect), mustRotationEffectJSON(t, baseline)) {
				t.Fatal("settlement changed other output fields")
			}
		})
	}
}

func mustRotationEffectJSON(t *testing.T, effect *rotationEffectOutput) []byte {
	t.Helper()
	data, err := json.Marshal(effect)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestInstalledRotationCommands(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(parent, "kc")
	build := []string{"build", "-o", binary}
	if runtime.GOOS == "darwin" && strings.HasPrefix(runtime.Version(), "go1.22") {
		build = append(build, "-ldflags=-linkmode=external")
	}
	if out, err := exec.Command("go", append(build, "../../cmd/kc")...).CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	invoke := func(want int, env []string, args ...string) rotationOutput {
		t.Helper()
		command := exec.Command(binary, append([]string{"rotation"}, args...)...)
		command.Env = append(append(os.Environ(), "HOME="+parent), env...)
		var stderr bytes.Buffer
		command.Stderr = &stderr
		raw, err := command.Output()
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != want || stderr.Len() != 0 {
			t.Fatalf("rotation exit=%d, want %d, stderr=%s, stdout=%s", code, want, stderr.String(), raw)
		}
		var output rotationOutput
		if err := json.Unmarshal(raw, &output); err != nil || output.Version != 1 {
			t.Fatalf("invalid rotation output: %s, %v", raw, err)
		}
		if bytes.Contains(raw, []byte("fixture-private-value")) {
			t.Fatal("rotation output leaked private text")
		}
		return output
	}
	input := writeRotationInputFixture(t, rotationInputFixture, 0600)
	out := invoke(0, nil, "plan", "--input", input)
	if out.Plan == nil || out.Plan.New.Number != 2 || len(out.Plan.RequiredConsumers) != 2 {
		t.Fatalf("missing plan metadata: %+v", out)
	}
	plan, err := buildRotationPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	store := journal.Store{Root: filepath.Join(parent, "journal")}
	_, err = rotation.SaveSession(store, rotation.SessionSnapshot{}, rotation.Session{
		Version: 1, Plan: plan, OldIdentityDigest: strings.Repeat("b", 64),
		Stage: journal.Intent{OperationID: "stage-1", PlanDigest: plan.ID, TargetDigest: strings.Repeat("c", 64), GrantDigest: strings.Repeat("d", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	out = invoke(0, nil, "status", "--journal", store.Root, "--plan", plan.ID)
	if out.Rotation == nil || out.Rotation.State != rotation.SessionPlanned || len(out.Rotation.MissingConsumers) != 2 {
		t.Fatalf("missing rotation status: %+v", out)
	}
	out = invoke(2, nil, "submit", "--provider-url=fixture-private-value", "fixture-issue")
	if out.Error == nil || out.Error.Code != "INVALID_ARGUMENTS" {
		t.Fatalf("unexpected flag error: %+v", out)
	}
	out = invoke(3, nil, "reconcile", "--profile", filepath.Join(parent, "missing"), "fixture-issue")
	if out.Error == nil || out.Error.Code != "WRITER_PROFILE_UNAVAILABLE" {
		t.Fatalf("unexpected profile error: %+v", out)
	}
	badProfile := filepath.Join(parent, "bad-writer.json")
	if err := os.WriteFile(badProfile, []byte(`{"v":1,"credential":"fixture-private-value"}`), 0600); err != nil {
		t.Fatal(err)
	}
	out = invoke(2, nil, "submit", "--profile", badProfile, "fixture-issue")
	if out.Error == nil || out.Error.Code != "INVALID_WRITER_PROFILE" {
		t.Fatalf("unexpected writer profile error: %+v", out)
	}
	if runtime.GOOS != "linux" {
		return
	}

	var calls atomic.Int32
	var behavior atomic.Int32
	const mutation = "fixture-mutation"
	key, err := effectgateway.ParseHexKey(strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/submit":
			var env effectgateway.Envelope
			if err := json.NewDecoder(r.Body).Decode(&env); err != nil || env.Request.Action != "issue" ||
				env.Request.MutationID != mutation || env.Signature != effectgateway.Sign(env.Request, key).Signature {
				t.Errorf("submit did not authenticate its registered operation")
			}
			switch behavior.Load() {
			case 1:
				w.WriteHeader(http.StatusConflict)
				_, _ = io.WriteString(w, `{"error":"STALE_GENERATION"}`)
				return
			case 2:
				// An unavailable response must yield one status lookup.
				_, _ = io.WriteString(w, `{}`)
				return
			}
			_ = json.NewEncoder(w).Encode(effectgateway.Receipt{
				MutationID: mutation, Status: effectgateway.StatusApplied, ProviderCalls: 1,
				OperationState: string(journal.Acknowledged), ProofDigest: strings.Repeat("a", 64),
				Note: "fixture-private-value", AuthoritySettlement: effectgateway.SettlementUnconfirmed,
			})
		case "/status", "/reconcile":
			var env effectgateway.ReadEnvelope
			if err := json.NewDecoder(r.Body).Decode(&env); err != nil || env.Request.MutationID != mutation ||
				env.Request.Action != strings.TrimPrefix(r.URL.Path, "/") || env.Signature != effectgateway.SignRead(env.Request, key).Signature {
				t.Errorf("read did not authenticate the original mutation")
			}
			if r.URL.Path == "/status" {
				_ = json.NewEncoder(w).Encode(effectgateway.Receipt{MutationID: mutation, Status: effectgateway.StatusUnknown, OperationState: string(journal.UnknownEffect)})
			} else {
				_ = json.NewEncoder(w).Encode(effectgateway.Receipt{
					MutationID: mutation, Status: effectgateway.StatusApplied, OperationState: string(journal.ObservedApplied), ProofDigest: strings.Repeat("a", 64),
				})
			}
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	write := func(name string, data []byte) string {
		t.Helper()
		path := filepath.Join(parent, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	origin := write("gateway.json", []byte(`{"v":1,"origin":"`+server.URL+`"}`))
	keyPath := write("session.key", []byte(effectgateway.HexKey(key)+"\n"))
	cert := write("gateway.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	profileRaw, err := json.Marshal(map[string]any{
		"v": 1, "gateway_config": origin, "session_key_file": keyPath,
		"goal_id": "fixture-goal", "owner": "fixture-owner", "session": "fixture-session", "generation": 1,
		"operations": []map[string]string{{
			"operation": "fixture-issue", "mutation_id": mutation, "provider": "cloudflare",
			"provider_operation": "create-account-token", "resource_digest": strings.Repeat("b", 64),
			"target_digest": strings.Repeat("c", 64), "plan_digest": plan.ID, "grant_digest": strings.Repeat("d", 64),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := write("writer.json", profileRaw)
	env := []string{"SSL_CERT_FILE=" + cert}
	out = invoke(2, env, "submit", "--profile", profile, "different-operation")
	if out.Error == nil || out.Error.Code != "OPERATION_NOT_REGISTERED" || calls.Load() != 0 {
		t.Fatalf("unregistered operation reached the Gateway: %+v", out)
	}
	out = invoke(3, env, "submit", "--profile", profile, "fixture-issue")
	if out.Effect == nil || out.Effect.Outcome != "ACKNOWLEDGED_UNVERIFIED" || out.Effect.StatusLookup ||
		out.Effect.AuthoritySettlement != effectgateway.SettlementUnconfirmed {
		t.Fatalf("acknowledgement was conflated with verification: %+v", out)
	}
	out = invoke(0, env, "status", "--profile", profile, "fixture-issue")
	if out.Effect == nil || out.Effect.Outcome != "UNKNOWN_EFFECT" {
		t.Fatalf("missing unknown status: %+v", out)
	}
	out = invoke(0, env, "reconcile", "--profile", profile, "fixture-issue")
	if out.Effect == nil || out.Effect.Outcome != "OBSERVED_APPLIED" {
		t.Fatalf("missing independent reconciliation: %+v", out)
	}
	behavior.Store(1)
	out = invoke(5, env, "submit", "--profile", profile, "fixture-issue")
	if out.Error == nil || out.Error.Code != "AUTHORIZATION_DENIED" {
		t.Fatalf("missing authorization denial: %+v", out)
	}
	behavior.Store(2)
	before := calls.Load()
	out = invoke(3, env, "submit", "--profile", profile, "fixture-issue")
	if out.Effect == nil || !out.Effect.StatusLookup || out.Effect.Outcome != "UNKNOWN_EFFECT" || out.Effect.AuthoritySettlement != "" || calls.Load() != before+2 {
		t.Fatalf("ambiguous submit did not read status exactly once: %+v, calls=%d", out, calls.Load()-before)
	}
}
