package file_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/config"

	_ "github.com/camem2442/keycli/internal/backend/file"
)

func TestFileStoreRoundtrip(t *testing.T) {
	dir := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_KEY", key)

	path := filepath.Join(dir, "store.age")
	cfg := config.Config{
		Order: []string{"store"},
		Backend: map[string]config.BackendSpec{
			"store": {Type: "file", Path: path},
		},
	}
	deps := backend.Deps{Home: dir, Config: cfg}
	backends, err := backend.Build(context.Background(), cfg, deps)
	if err != nil || len(backends) != 1 {
		t.Fatalf("build: %v len=%d", err, len(backends))
	}
	b := backends[0]
	ctx := context.Background()
	if err := b.Set(ctx, "demo", []byte("value"), backend.Meta{Note: "n"}); err != nil {
		t.Fatal(err)
	}
	got, err := b.Get(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "value" {
		t.Fatalf("got %q", got)
	}
	items, err := b.List(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("list: %v %d", err, len(items))
	}
	if err := b.Delete(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	_, err = b.Get(ctx, "demo")
	if err == nil {
		t.Fatal("expected not found")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", info.Mode())
	}
}
