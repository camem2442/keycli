package resolve_test

import (
	"context"
	"os"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/resolve"

	_ "github.com/camem2442/keycli/internal/backend/env"
	_ "github.com/camem2442/keycli/internal/backend/file"
)

func TestResolveAliasEnv(t *testing.T) {
	t.Setenv("GH_TOKEN", "from-env")
	dir := t.TempDir()
	cfg := config.Default(dir)
	backends, err := backend.Build(context.Background(), cfg, backend.Deps{Home: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	r := resolve.New(cfg, backends, dir, nil)
	val, _, err := r.Get(context.Background(), "gh")
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "from-env" {
		t.Fatalf("got %q", val)
	}
}

func TestNotFoundCode(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	os.Unsetenv("GH_TOKEN")
	os.Unsetenv("GITHUB_TOKEN")
	backends, err := backend.Build(context.Background(), cfg, backend.Deps{Home: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	r := resolve.New(cfg, backends, dir, nil)
	_, _, err = r.Get(context.Background(), "missing")
	if kcerrors.Code(err) != kcerrors.ExitNotFound {
		t.Fatalf("code %d err %v", kcerrors.Code(err), err)
	}
}
