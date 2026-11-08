package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAmbiguousAliasesBlockExecutionAndDigest(t *testing.T) {
	cases := []struct {
		name  string
		alias string
	}{
		{"shared", "allowed = [\"shared\"]\ndenied = [\"shared\"]\n"},
		{"case", "allowed = [\"Shared\"]\ndenied = [\"SHARED\"]\n"},
		{"primary", "allowed = [\"denied\"]\ndenied = []\n"},
		{"primary-case", "Allowed = []\nallowed = []\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			cfgPath := filepath.Join(home, "config.toml")
			marker := filepath.Join(home, "child-started")
			cfg := "order = [\"env\"]\nmiddleware = [\"audit\", \"policy\"]\n[policy.agent.codex]\nallow = [\"allowed\"]\ndeny = [\"denied\"]\n[alias]\n" + tc.alias
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			env := []string{"HOME=" + home, "KC_CONFIG=" + cfgPath, "KC_AGENT=codex"}
			code, out, _ := runKCSplit(env, "", "policy", "validate", "--json")
			var validation struct {
				OK     bool     `json:"ok"`
				Errors []string `json:"errors"`
			}
			if err := json.Unmarshal([]byte(out), &validation); err != nil {
				t.Fatal(err)
			}
			if code != 1 || validation.OK || len(validation.Errors) == 0 {
				t.Fatalf("ambiguous aliases passed validation: code=%d errors=%v", code, validation.Errors)
			}
			code, out, _ = runKCSplit(env, "", "policy", "show", "--json")
			var shown map[string]any
			if err := json.Unmarshal([]byte(out), &shown); err != nil {
				t.Fatal(err)
			}
			if code != 0 {
				t.Fatal("policy show must remain available for diagnostics")
			}
			if _, ok := shown["policy_digest"]; ok {
				t.Fatal("ambiguous aliases must not receive a policy digest")
			}
			code, _, _ = runKCSplit(env, "", "exec", "--require", "--only", "allowed", "--", "sh", "-c", ": > \"$1\"", "sh", marker)
			if code != 1 {
				t.Fatalf("expected configuration rejection before resolution, got %d", code)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("child ran despite ambiguous aliases")
			}
		})
	}
}
