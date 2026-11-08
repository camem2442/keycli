package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The CLI wire field must be machine-readable, additive to policy show v1,
// and absent when a malformed policy is retained for diagnostics.
func TestPolicyShowDigestWire(t *testing.T) {
	home := t.TempDir()
	cfgPath := filepath.Join(home, "config.toml")
	cfg := `order = ["env"]
middleware = ["audit", "policy"]
[alias]
github = ["GH_TOKEN"]
[policy.agent.codex]
allow = ["*"]
deny = ["private_*"]
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_CONFIG=" + cfgPath, "KC_AGENT=codex"}
	code, out, stderr := runKCSplit(env, "", "policy", "show", "--agent", "codex", "--json")
	if code != 0 {
		t.Fatalf("show failed %d %s", code, stderr)
	}
	var payload struct {
		V            int    `json:"v"`
		Agent        string `json:"agent"`
		Enforced     bool   `json:"enforced"`
		Source       string `json:"source"`
		PolicyDigest string `json:"policy_digest"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.V != 1 || payload.Agent != "codex" || !payload.Enforced || payload.Source != "explicit" {
		t.Fatalf("unexpected policy identity fields: %+v", payload)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(payload.PolicyDigest) {
		t.Fatalf("missing valid policy_digest: %q", payload.PolicyDigest)
	}
	code, repeat, stderr := runKCSplit(env, "", "policy", "show", "--agent", "codex", "--json")
	if code != 0 || out != repeat {
		t.Fatalf("policy show must be stable %d %s", code, stderr)
	}
	if err := os.WriteFile(cfgPath, []byte(`middleware = ["policy"]
[policy.agent.codex]
allow = ["*"]
deny = ["broken_["]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = runKCSplit(env, "", "policy", "show", "--agent", "codex", "--json")
	if code != 0 {
		t.Fatalf("show must remain a diagnostic command %d %s", code, stderr)
	}
	var invalid map[string]any
	if err := json.Unmarshal([]byte(out), &invalid); err != nil {
		t.Fatal(err)
	}
	if _, ok := invalid["policy_digest"]; ok {
		t.Fatal("invalid rules must never claim a valid policy digest")
	}
}
