package env_test

import (
	"context"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/config"

	_ "github.com/camem2442/keycli/internal/backend/env"
)

func TestEnvListExcludesKCInternals(t *testing.T) {
	t.Setenv("KC_KEY", "age-internal")
	t.Setenv("KC_AGENT", "codex")
	t.Setenv("KC_TEST_KEYCHAIN", "1")
	t.Setenv("GH_TOKEN", "from-env")
	t.Setenv("MY_TOKEN", "mine")
	dir := t.TempDir()
	cfg := config.Config{
		Order:   []string{"env"},
		Backend: map[string]config.BackendSpec{"env": {Type: "env"}},
		Alias:   config.Default(dir).Alias,
	}
	cfg.Alias["mytoken"] = []string{"MY_TOKEN"}
	bs, err := backend.Build(context.Background(), cfg, backend.Deps{Home: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	items, err := bs[0].List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, it := range items {
		got[it.Name] = true
	}
	if !got["github"] || !got["mytoken"] {
		t.Fatalf("alias entries missing: %#v", got)
	}
	for _, bad := range []string{"key", "agent", "test_keychain"} {
		if got[bad] {
			t.Fatalf("internal KC_* variable leaked into ls as %q: %#v", bad, got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("unexpected items: %#v", got)
	}
}

func TestEnvGetGHToken(t *testing.T) {
	t.Setenv("KC_KEY", "")
	t.Setenv("GH_TOKEN", "from-env")
	dir := t.TempDir()
	cfg := config.Config{
		Order:   []string{"env"},
		Backend: map[string]config.BackendSpec{"env": {Type: "env"}},
		Alias:   config.Default(dir).Alias,
	}
	bs, err := backend.Build(context.Background(), cfg, backend.Deps{Home: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	v, err := bs[0].Get(context.Background(), "GH_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if string(v) != "from-env" {
		t.Fatalf("got %q", v)
	}
	items, err := bs[0].List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "github" {
		t.Fatalf("list %+v", items)
	}
}
