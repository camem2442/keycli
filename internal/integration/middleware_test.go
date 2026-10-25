package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/backend/file"
)

// TestPolicyRulesWithoutMiddleware covers P1-7: rules alone must not mark
// names denied in ls or block get unless the policy middleware is enabled.
func TestPolicyRulesWithoutMiddleware(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	configToml := `
allow_test_hooks = true
order = ["store"]

[backend.store]
type = "file"
path = "` + storePath + `"

[policy.agent.ci]
allow = ["allowed"]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + key, "KC_AGENT=ci"}
	for _, name := range []string{"allowed", "denied"} {
		if code, out := runKC(env, "set", name); code != 0 {
			t.Fatalf("set %s: %d %s", name, code, out)
		}
	}

	if code, out := runKC(env, "get", "denied"); code != 0 {
		t.Fatalf("rules without middleware must not block get: %d %s", code, out)
	}
	code, out := runKC(env, "ls", "--json", "--all")
	if code != 0 {
		t.Fatalf("ls: %d %s", code, out)
	}
	var payload struct {
		Items []struct {
			Name         string `json:"name"`
			PolicyDenied bool   `json:"policy_denied"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	for _, it := range payload.Items {
		if it.PolicyDenied {
			t.Fatalf("rules without middleware marked %q denied", it.Name)
		}
	}
	if code, out := runKC(env, "policy", "check", "denied"); code != 0 || !strings.Contains(out, "not enforced") {
		t.Fatalf("policy check should report not enforced: %d %q", code, out)
	}
}

func TestMiddlewarePolicyAndAudit(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	auditPath := filepath.Join(home, ".kc", "audit.log")
	configToml := `
allow_test_hooks = true
middleware = ["audit", "policy"]
order = ["env", "store"]

[backend.env]
type = "env"

[backend.store]
type = "file"
path = "` + storePath + `"

[audit]
path = "` + auditPath + `"

[policy.agent.codex]
allow = ["github", "groq*"]

[alias]
github = ["gh", "GITHUB_TOKEN", "GH_TOKEN"]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatal(err)
	}
	baseEnv := []string{
		"HOME=" + home,
		"KC_KEY=" + key,
		"KC_AGENT=codex",
		"GH_TOKEN=gh-secret",
		"GROQ_API_KEY=groq-secret",
	}

	code, _ := runKC(baseEnv, "set", "openai")
	if code != 0 {
		t.Fatalf("set openai %d", code)
	}
	code, _ = runKC(baseEnv, "get", "github")
	if code != 0 {
		t.Fatalf("github get code %d", code)
	}
	code, out := runKC(baseEnv, "get", "openai")
	if code != 5 {
		t.Fatalf("denied code %d out %q", code, out)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "gh-secret") || strings.Contains(string(data), "groq-secret") {
		t.Fatal("audit leaked value")
	}
	if !strings.Contains(string(data), `"outcome":"denied"`) {
		t.Fatal("missing denied audit", string(data))
	}

	code, out = runKC(baseEnv, "ls", "--json")
	if code != 0 {
		t.Fatal(out)
	}
	var payload struct {
		Items []struct {
			Name         string `json:"name"`
			PolicyDenied bool   `json:"policy_denied"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	var sawDenied bool
	for _, it := range payload.Items {
		if it.Name == "github" && it.PolicyDenied {
			t.Fatal("github should not be denied")
		}
		if it.PolicyDenied {
			sawDenied = true
		}
	}
	if !sawDenied {
		t.Fatal("expected policy_denied on some item")
	}

	code, _ = runKC(baseEnv, "policy", "check", "github")
	if code != 0 {
		t.Fatalf("policy check github %d", code)
	}
	code, _ = runKC(baseEnv, "policy", "check", "openai")
	if code != 5 {
		t.Fatalf("policy check openai %d", code)
	}
}
