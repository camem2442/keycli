package external_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/external"
	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/plugin"
)

func buildPlugin(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join("testdata", "plugins", name)
	out := filepath.Join(t.TempDir(), "kc-backend-"+name)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = root
	if outB, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, outB)
	}
	return out
}

func TestExternalBackendGetList(t *testing.T) {
	home := t.TempDir()
	bin := buildPlugin(t, "ok")
	pluginsDir := filepath.Join(home, ".kc", "plugins")
	_ = os.MkdirAll(pluginsDir, 0o755)
	_ = os.Symlink(bin, filepath.Join(pluginsDir, "kc-backend-ok"))

	cfg := config.Default(home)
	cfg.Order = []string{"plug"}
	cfg.Backend["plug"] = config.BackendSpec{Type: "external", Plugin: "ok", Timeout: "5s"}

	bs, err := backend.Build(context.Background(), cfg, backend.Deps{Home: home, Config: cfg})
	if err != nil || len(bs) != 1 {
		t.Fatalf("build: %v len=%d", err, len(bs))
	}
	b := bs[0]
	val, err := b.Get(context.Background(), "found")
	if err != nil || string(val) != "sekrit" {
		t.Fatalf("get: %v %q", err, val)
	}
	items, err := b.List(context.Background())
	if err != nil || len(items) != 1 || items[0].Name != "found" {
		t.Fatalf("list: %v %+v", err, items)
	}
}

func TestMalformedPlugin(t *testing.T) {
	home := t.TempDir()
	bin := buildPlugin(t, "badjson")
	cfg := config.Default(home)
	cfg.Order = []string{"plug"}
	cfg.Backend["plug"] = config.BackendSpec{Type: "external", Command: bin, Timeout: "2s"}

	bs, err := backend.Build(context.Background(), cfg, backend.Deps{Home: home, Config: cfg})
	if err != nil || len(bs) != 1 {
		t.Fatalf("malformed plugin must not fail Build: %v len=%d", err, len(bs))
	}
	ok, reason, err := bs[0].Available(context.Background())
	if err != nil || ok || !strings.Contains(reason, "invalid JSON") {
		t.Fatalf("want invalid JSON reason, got ok=%v err=%v reason=%q", ok, err, reason)
	}
}

func TestPluginTimeoutIsolated(t *testing.T) {
	home := t.TempDir()
	bin := buildPlugin(t, "slow")
	cfg := config.Default(home)
	cfg.Order = []string{"plug", "store"}
	cfg.Backend["plug"] = config.BackendSpec{Type: "external", Command: bin, Timeout: "100ms"}
	cfg.Backend["store"] = config.BackendSpec{Type: "file", Path: filepath.Join(home, ".kc", "store.age")}
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_KEY", key)

	bs, err := backend.Build(context.Background(), cfg, backend.Deps{Home: home, Config: cfg})
	if err != nil {
		t.Fatalf("broken plugin must not fail Build: %v", err)
	}
	if len(bs) != 2 {
		t.Fatalf("backends %d, want 2", len(bs))
	}
	ok, reason, err := bs[0].Available(context.Background())
	if err != nil || ok {
		t.Fatalf("slow plugin should be unavailable: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(reason, "timeout") {
		t.Fatalf("reason %q", reason)
	}
	ok, _, err = bs[1].Available(context.Background())
	if err != nil || !ok {
		t.Fatalf("file backend must remain available: ok=%v err=%v", ok, err)
	}
	if err := bs[1].Set(context.Background(), "demo", []byte("v"), backend.Meta{}); err != nil {
		t.Fatalf("file backend write: %v", err)
	}
}

func TestMissingPluginIsolated(t *testing.T) {
	home := t.TempDir()
	cfg := config.Default(home)
	cfg.Order = []string{"plug", "store"}
	cfg.Backend["plug"] = config.BackendSpec{Type: "external", Plugin: "definitely-not-installed"}
	cfg.Backend["store"] = config.BackendSpec{Type: "file", Path: filepath.Join(home, ".kc", "store.age")}

	bs, err := backend.Build(context.Background(), cfg, backend.Deps{Home: home, Config: cfg})
	if err != nil {
		t.Fatalf("missing plugin must not fail Build: %v", err)
	}
	if len(bs) != 2 {
		t.Fatalf("backends %d, want 2", len(bs))
	}
	ok, reason, err := bs[0].Available(context.Background())
	if err != nil || ok {
		t.Fatalf("missing plugin should be unavailable: ok=%v err=%v", ok, err)
	}
	if reason == "" {
		t.Fatal("missing plugin should explain why")
	}
}

func TestDisabledPluginSkipped(t *testing.T) {
	home := t.TempDir()
	bin := buildPlugin(t, "ok")
	pluginsDir := filepath.Join(home, ".kc", "plugins")
	_ = os.MkdirAll(pluginsDir, 0o755)
	_ = os.Symlink(bin, filepath.Join(pluginsDir, "kc-backend-ok"))

	cfg := config.Default(home)
	cfg.Order = []string{"plug", "store"}
	disabled := false
	cfg.Backend["plug"] = config.BackendSpec{Type: "external", Plugin: "ok", Enabled: &disabled}
	cfg.Backend["store"] = config.BackendSpec{Type: "file", Path: filepath.Join(home, ".kc", "store.age")}

	bs, err := backend.Build(context.Background(), cfg, backend.Deps{Home: home, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 1 || bs[0].Kind() != "file" {
		t.Fatalf("disabled external should be skipped, got %d backends", len(bs))
	}

	entries, err := plugin.List(context.Background(), home, cfg, external.ExecRunner{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var okEnt *plugin.Entry
	for _, e := range entries {
		if e.Name == "ok" {
			okEnt = &e
			break
		}
	}
	if okEnt == nil {
		t.Fatal("plugin not discovered on PATH")
	}
	if okEnt.Enabled {
		t.Fatal("should show disabled")
	}
}

func TestPATHPluginNotAutoEnabled(t *testing.T) {
	home := t.TempDir()
	bin := buildPlugin(t, "ok")
	dir := filepath.Join(home, "bin")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.Symlink(bin, filepath.Join(dir, "kc-backend-ok"))

	cfg := config.Default(home)
	cfg.Order = []string{"store"}
	// no external backend in config — only file store

	bs, err := backend.Build(context.Background(), cfg, backend.Deps{Home: home, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bs {
		if b.Kind() == "external" {
			t.Fatal("PATH plugin must not run without config")
		}
	}
}

func TestMapErrnoNotFound(t *testing.T) {
	err := external.MapErrno("not_found", "x")
	if kcerrors.Code(err) != kcerrors.ExitNotFound {
		t.Fatalf("code %d", kcerrors.Code(err))
	}
}
