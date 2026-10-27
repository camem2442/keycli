package opencode

import (
	"encoding/json"
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
	if ShouldSyncBackgroundService([]string{"/opt/homebrew/bin/opencode"}) {
		t.Fatal("bare opencode should not sync")
	}
	if ShouldSyncBackgroundService([]string{"opencode", "--version"}) {
		t.Fatal("version should skip sync")
	}
	if ShouldSyncBackgroundService([]string{"opencode", "service", "status"}) {
		t.Fatal("service status should skip sync")
	}
	if ShouldSyncBackgroundService([]string{"opencode", "run", "--standalone", "hi"}) {
		t.Fatal("standalone should skip sync")
	}
	if ShouldSyncBackgroundService([]string{"not-opencode", "run"}) {
		t.Fatal("wrong binary")
	}
}

func TestEffectiveProxyAPIKey_inheritedAndInjected(t *testing.T) {
	env := []string{
		"PROXY_API_KEY=inherited",
		"FOO=bar",
		"PROXY_API_KEY=injected",
	}
	got := EffectiveProxyAPIKey(env)
	if got != "injected" {
		t.Fatalf("got %q", got)
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

func TestSyncBackgroundServiceEnv_noSecretArgv(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	logPath := filepath.Join(dir, "calls.log")
	bin := filepath.Join(dir, "opencode")
	script := `#!/bin/sh
echo "$@" >> "` + logPath + `"
case "$1" in
debug)
  case "$3" in
  config) echo "` + configDir + `"; exit 0 ;;
  esac
  ;;
service)
  exit 0
  ;;
esac
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	const secret = "redacted-test-key"
	if err := SyncBackgroundServiceEnv(bin, secret); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	joined := string(data)
	if strings.Contains(joined, secret) {
		t.Fatalf("secret leaked in argv log: %q", joined)
	}
	if !strings.Contains(joined, "service stop") || !strings.Contains(joined, "service restart") {
		t.Fatalf("expected stop/restart: %q", joined)
	}
	cfgPath := filepath.Join(configDir, "service.json")
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]map[string]string
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["env"][EnvAPIKey] != secret {
		t.Fatalf("env not written: %v", cfg)
	}
}

func TestSanitizeServiceOutput_redactsSecret(t *testing.T) {
	out := sanitizeServiceOutput([]byte("failed: sk-live-abc"), nil, "sk-live-abc")
	if strings.Contains(out, "sk-live-abc") {
		t.Fatalf("secret in output: %q", out)
	}
	if !strings.Contains(out, "[redacted]") {
		t.Fatalf("expected redaction: %q", out)
	}
}
