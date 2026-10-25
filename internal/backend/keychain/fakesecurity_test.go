package keychain_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/keychain"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"

	_ "github.com/camem2442/keycli/internal/backend/keychain"
)

var fakeSecBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kc-fakesec-build")
	if err != nil {
		panic(err)
	}
	fakeSecBin = filepath.Join(dir, "fakesecurity")
	out, err := exec.Command("go", "build", "-o", fakeSecBin, "../../../cmd/fakesecurity").CombinedOutput()
	if err != nil {
		panic(string(out))
	}
	os.Exit(m.Run())
}

func TestFakeSecurityAICoreReadOnly(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	argvLog := filepath.Join(dir, "argv.log")
	seed, _ := json.Marshal(map[string]any{
		"entries": map[string]string{
			"local.study.ai.cerebras\tcerebras_api_key": "abc123",
			"kc.secret.demo\tkc":                        "demo-value",
		},
	})
	if err := os.WriteFile(state, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_SECURITY", fakeSecBin)
	t.Setenv("KC_FAKE_SECURITY_STATE", state)
	t.Setenv("KC_FAKE_SECURITY_ARGV_LOG", argvLog)
	t.Setenv("KC_TEST_KEYCHAIN", "1")

	cfg := config.Config{
		AllowTestHooks: true,
		Order:          []string{"work"},
		Backend: map[string]config.BackendSpec{
			"work": {Type: "keychain", ServicePrefix: "local.study.ai"},
		},
	}
	deps := backend.Deps{
		Home:                     dir,
		Config:                   cfg,
		SecurityRunner:           keychain.ExecRunner{Config: cfg},
		AllowKeychainOnNonDarwin: true,
	}
	bs, err := backend.Build(context.Background(), cfg, deps)
	if err != nil {
		t.Fatal(err)
	}
	kb := bs[0]
	val, err := kb.Get(context.Background(), "cerebras")
	if err != nil || string(val) != "abc123" {
		t.Fatalf("get cerebras len=%d err=%v", len(val), err)
	}
	items, err := kb.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	if !contains(names, "cerebras") || !contains(names, "demo") {
		t.Fatalf("list missing real-dump entries: %#v", names)
	}
	err = kb.Set(context.Background(), "cerebras", []byte("nope"), backend.Meta{})
	if kcerrors.Code(err) != kcerrors.ExitDenied {
		t.Fatalf("expected denied, got %v", err)
	}
	log, _ := os.ReadFile(argvLog)
	if strings.Contains(string(log), "abc123") || strings.Contains(string(log), "nope") {
		t.Fatal("secret appeared in argv log")
	}
}

// TestKeychainDottedNameRoundTrip covers P1-5: dotted names use kc.secret.*,
// show up in List, and cannot overwrite the reserved kc.age identity.
func TestKeychainDottedNameRoundTrip(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "state.json")
	seed, _ := json.Marshal(map[string]any{
		"entries": map[string]string{
			"kc.secret.foo.bar\tkc": "dotted-value",
			"kc.age\tkc":            "IDENTITY-DO-NOT-TOUCH",
		},
	})
	if err := os.WriteFile(state, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_SECURITY", fakeSecBin)
	t.Setenv("KC_FAKE_SECURITY_STATE", state)
	t.Setenv("KC_TEST_KEYCHAIN", "1")

	cfg := config.Config{
		AllowTestHooks: true,
		Order:          []string{"work"},
		Backend:        map[string]config.BackendSpec{"work": {Type: "keychain"}},
	}
	bs, err := backend.Build(context.Background(), cfg, backend.Deps{
		Home: dir, Config: cfg, SecurityRunner: keychain.ExecRunner{Config: cfg}, AllowKeychainOnNonDarwin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	kb := bs[0]

	val, err := kb.Get(context.Background(), "foo.bar")
	if err != nil || string(val) != "dotted-value" {
		t.Fatalf("dotted get: %q %v", val, err)
	}
	items, err := kb.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, it := range items {
		if it.Name == "foo.bar" {
			found = true
		}
	}
	if !found {
		t.Fatalf("dotted name missing from list: %+v", items)
	}

	if err := kb.Set(context.Background(), "demo.bar", []byte("new-value"), backend.Meta{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "kc.secret.demo.bar") || strings.Contains(string(data), `"demo.bar\tkc"`) {
		t.Fatalf("write did not use kc.secret prefix: %s", data)
	}
	if !strings.Contains(string(data), "IDENTITY-DO-NOT-TOUCH") {
		t.Fatal("identity entry was modified")
	}

	if err := kb.Set(context.Background(), "kc.age", []byte("overwrite"), backend.Meta{}); kcerrors.Code(err) != kcerrors.ExitUsage {
		t.Fatalf("kc.age set must be reserved: %v", err)
	}
	if err := kb.Delete(context.Background(), "kc.age"); kcerrors.Code(err) != kcerrors.ExitUsage {
		t.Fatalf("kc.age delete must be reserved: %v", err)
	}
	data, err = os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "IDENTITY-DO-NOT-TOUCH") {
		t.Fatal("identity entry was overwritten or deleted")
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func TestFakeSecuritySandboxExit3(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KC_SECURITY", fakeSecBin)
	t.Setenv("KC_FAKE_SECURITY_STATE", filepath.Join(dir, "state.json"))
	t.Setenv("KC_FAKE_SECURITY_SANDBOX", "1")
	t.Setenv("KC_TEST_KEYCHAIN", "1")
	cfg := config.Config{
		AllowTestHooks: true,
		Order:          []string{"work"},
		Backend:        map[string]config.BackendSpec{"work": {Type: "keychain"}},
	}
	bs, err := backend.Build(context.Background(), cfg, backend.Deps{
		Home: dir, Config: cfg, SecurityRunner: keychain.ExecRunner{Config: cfg}, AllowKeychainOnNonDarwin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bs[0].Get(context.Background(), "openai")
	if kcerrors.Code(err) != kcerrors.ExitBlocked {
		t.Fatalf("expected blocked, got %v", err)
	}
}
