package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
)

func loadWith(t *testing.T, body string) (config.Config, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_CONFIG", path)
	cfg, _, err := config.Load(dir)
	return cfg, err
}

// A malformed glob must not abort config loading: diagnostics still need to
// run. Enforcement reads ValidatePolicyPatterns and fails closed.
func TestLoadDefersPolicyPatternValidation(t *testing.T) {
	cfg, err := loadWith(t, `
middleware = ["policy"]

[policy.agent.cursor]
allow = ["*"]
deny = ["sogang-[abc"]
`)
	if err != nil {
		t.Fatalf("load must succeed so diagnostics work, got %v", err)
	}
	perr := cfg.ValidatePolicyPatterns()
	if perr == nil {
		t.Fatal("malformed deny glob must be reported by ValidatePolicyPatterns")
	}
	msg := perr.Error()
	if !strings.Contains(msg, "deny") || !strings.Contains(msg, "sogang-[abc") {
		t.Fatalf("error should name the deny pattern, got %q", msg)
	}
}

func TestValidatePolicyPatternsRejectsMalformedAllow(t *testing.T) {
	cfg, err := loadWith(t, `
[policy.agent.cursor]
allow = ["dev_[abc"]
`)
	if err != nil {
		t.Fatal(err)
	}
	perr := cfg.ValidatePolicyPatterns()
	if perr == nil || !strings.Contains(perr.Error(), "allow") {
		t.Fatalf("expected allow-list error, got %v", perr)
	}
}

func TestValidatePolicyPatternsRejectsEmptyPattern(t *testing.T) {
	cfg, err := loadWith(t, `
[policy.agent.cursor]
allow = ["*"]
deny = ["  "]
`)
	if err != nil {
		t.Fatal(err)
	}
	perr := cfg.ValidatePolicyPatterns()
	if perr == nil || !strings.Contains(perr.Error(), "empty pattern") {
		t.Fatalf("expected empty-pattern error, got %v", perr)
	}
}

func TestValidatePolicyPatternsAcceptsValidGlobs(t *testing.T) {
	cfg, err := loadWith(t, `
[policy.agent.cursor]
allow = ["*"]
deny = ["github", "sogang-*", "viewer_*", "a?b"]
`)
	if err != nil {
		t.Fatal(err)
	}
	if perr := cfg.ValidatePolicyPatterns(); perr != nil {
		t.Fatalf("valid globs must pass, got %v", perr)
	}
}

// Broken TOML is still a load error; only policy globs are deferred.
func TestLoadStillRejectsBrokenTOML(t *testing.T) {
	if _, err := loadWith(t, "[policy.agent.cursor\nallow = ["); err == nil {
		t.Fatal("broken TOML must still fail at load")
	}
}
