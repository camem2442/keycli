package gh_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/target"
	"github.com/camem2442/keycli/internal/target/gh"
)

func TestPushStdinNotArgv(t *testing.T) {
	for _, name := range []string{"KC_KEY", "KC_AGENT", "KC_EFFECT_KEY", "KC_PROVIDER_EFFECTS", "GH_TOKEN"} {
		t.Setenv(name, "fixture")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "argv.log")
	script := filepath.Join(dir, "fake-gh.sh")
	content := `#!/bin/sh
test -z "${KC_KEY+x}${KC_AGENT+x}${KC_EFFECT_KEY+x}${KC_PROVIDER_EFFECTS+x}" && test "$GH_TOKEN" = fixture || exit 91
echo "$@" >> "` + log + `"
cat > /dev/null
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}

	err := gh.Push(context.Background(), gh.Options{GhBin: script, User: true, App: "codespaces"}, config.Config{}, []target.Secret{
		{Name: "MY_TOKEN", Value: []byte("super-secret")},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(log)
	line := strings.TrimSpace(string(raw))
	if strings.Contains(line, "super-secret") {
		t.Fatalf("secret leaked to argv/log: %s", line)
	}
	if !strings.Contains(line, "secret set MY_TOKEN") {
		t.Fatalf("unexpected argv: %s", line)
	}
}

func TestGhBinFromOpts(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
}
