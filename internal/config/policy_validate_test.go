package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
)

func loadWith(t *testing.T, body string) error {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_CONFIG", path)
	_, _, err := config.Load(dir)
	return err
}

// A malformed deny glob used to fail silently in path.Match, so the deny rule
// matched nothing and the broad allow underneath it applied.
func TestLoadRejectsMalformedDenyGlob(t *testing.T) {
	err := loadWith(t, `
middleware = ["policy"]

[policy.agent.cursor]
allow = ["*"]
deny = ["sogang-[abc"]
`)
	if err == nil {
		t.Fatal("malformed deny glob must be rejected")
	}
	msg := err.Error()
	if !strings.Contains(msg, "deny") || !strings.Contains(msg, "sogang-[abc") {
		t.Fatalf("error should name the deny pattern, got %q", msg)
	}
}

func TestLoadRejectsMalformedAllowGlob(t *testing.T) {
	err := loadWith(t, `
[policy.agent.cursor]
allow = ["dev_[abc"]
`)
	if err == nil {
		t.Fatal("malformed allow glob must be rejected")
	}
	if !strings.Contains(err.Error(), "allow") {
		t.Fatalf("error should name the allow list, got %q", err.Error())
	}
}

func TestLoadRejectsEmptyPattern(t *testing.T) {
	err := loadWith(t, `
[policy.agent.cursor]
allow = ["*"]
deny = ["  "]
`)
	if err == nil {
		t.Fatal("empty pattern must be rejected")
	}
	if !strings.Contains(err.Error(), "empty pattern") {
		t.Fatalf("unexpected error %q", err.Error())
	}
}

func TestLoadAcceptsValidGlobs(t *testing.T) {
	err := loadWith(t, `
[policy.agent.cursor]
allow = ["*"]
deny = ["github", "sogang-*", "viewer_*", "a?b"]
`)
	if err != nil {
		t.Fatalf("valid globs must load, got %v", err)
	}
}
