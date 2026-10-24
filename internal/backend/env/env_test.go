package env_test

import (
	"context"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/config"

	_ "github.com/camem2442/keycli/internal/backend/env"
)

func TestEnvGetGHToken(t *testing.T) {
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
