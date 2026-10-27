package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShouldSyncBackgroundService(t *testing.T) {
	if ShouldSyncBackgroundService(nil) || ShouldSyncBackgroundService([]string{}) {
		t.Fatal("empty argv")
	}
	if !ShouldSyncBackgroundService([]string{"opencode", "run", "-m", "x"}) {
		t.Fatal("expected sync for opencode run")
	}
	if !ShouldSyncBackgroundService([]string{"/opt/homebrew/bin/opencode"}) {
		t.Fatal("expected sync for bare opencode")
	}
	if ShouldSyncBackgroundService([]string{"opencode", "run", "--standalone", "hi"}) {
		t.Fatal("standalone should skip sync")
	}
	if ShouldSyncBackgroundService([]string{"not-opencode", "run"}) {
		t.Fatal("wrong binary")
	}
}

func TestProxyAPIKeyFromEnv(t *testing.T) {
	if ProxyAPIKeyFromEnv([]string{"FOO=bar"}) != "" {
		t.Fatal("missing key")
	}
	got := ProxyAPIKeyFromEnv([]string{"PROXY_API_KEY=secret-value"})
	if got != "secret-value" {
		t.Fatalf("got %q", got)
	}
}

func TestSyncBackgroundServiceEnv_invokesOpencode(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
echo "$@" >> "` + logPath + `"
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := SyncBackgroundServiceEnv(bin, "redacted-test-key"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(strings.Fields(string(data)), " ")
	if !strings.Contains(joined, "service set env PROXY_API_KEY redacted-test-key") {
		t.Fatalf("set call missing: %q", joined)
	}
	if !strings.Contains(joined, "service restart") {
		t.Fatalf("restart call missing: %q", joined)
	}
}
