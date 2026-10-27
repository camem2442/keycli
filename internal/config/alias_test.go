package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/camem2442/keycli/internal/config"
)

func TestLoadEnsuresCursorAPIKeyAlias(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[alias]\ngithub = [\"gh\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_CONFIG", cfgPath)
	c, _, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	als, ok := c.Alias["cursor-api-key"]
	if !ok {
		t.Fatal("cursor-api-key alias missing")
	}
	if len(als) != 1 || als[0] != "cursor_api_key" {
		t.Fatalf("alias %v", als)
	}
}
