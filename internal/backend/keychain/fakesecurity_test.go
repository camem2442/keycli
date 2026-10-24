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
		Order: []string{"work"},
		Backend: map[string]config.BackendSpec{
			"work": {Type: "keychain", ServicePrefix: "local.study.ai"},
		},
	}
	deps := backend.Deps{
		Home:                     dir,
		Config:                   cfg,
		SecurityRunner:           keychain.ExecRunner{},
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
	err = kb.Set(context.Background(), "cerebras", []byte("nope"), backend.Meta{})
	if kcerrors.Code(err) != kcerrors.ExitDenied {
		t.Fatalf("expected denied, got %v", err)
	}
	log, _ := os.ReadFile(argvLog)
	if strings.Contains(string(log), "abc123") || strings.Contains(string(log), "nope") {
		t.Fatal("secret appeared in argv log")
	}
}

func TestFakeSecuritySandboxExit3(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KC_SECURITY", fakeSecBin)
	t.Setenv("KC_FAKE_SECURITY_STATE", filepath.Join(dir, "state.json"))
	t.Setenv("KC_FAKE_SECURITY_SANDBOX", "1")
	t.Setenv("KC_TEST_KEYCHAIN", "1")
	cfg := config.Config{
		Order:   []string{"work"},
		Backend: map[string]config.BackendSpec{"work": {Type: "keychain"}},
	}
	bs, err := backend.Build(context.Background(), cfg, backend.Deps{
		Home: dir, Config: cfg, SecurityRunner: keychain.ExecRunner{}, AllowKeychainOnNonDarwin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = bs[0].Get(context.Background(), "openai")
	if kcerrors.Code(err) != kcerrors.ExitBlocked {
		t.Fatalf("expected blocked, got %v", err)
	}
}
