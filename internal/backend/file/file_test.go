package file_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
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

func TestSetIfAbsentDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_KEY", key)
	cfg := config.Config{Order: []string{"store"}, Backend: map[string]config.BackendSpec{"store": {Type: "file", Path: filepath.Join(dir, "store.age")}}}
	backends, err := backend.Build(context.Background(), cfg, backend.Deps{Home: dir, Config: cfg})
	if err != nil || len(backends) != 1 {
		t.Fatalf("build: %v", err)
	}
	store, ok := backends[0].(*file.Store)
	if !ok {
		t.Fatal("not file store")
	}
	ctx := context.Background()
	if err := store.SetIfAbsent(ctx, "demo", []byte("first"), backend.Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetIfAbsent(ctx, "DEMO", []byte("second"), backend.Meta{}); err == nil {
		t.Fatal("overwrite allowed")
	}
	got, err := store.Get(ctx, "demo")
	if err != nil || string(got) != "first" {
		t.Fatal("original value not retained")
	}
}

func TestSetIfAbsentConcurrentStores(t *testing.T) {
	dir := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_KEY", key)
	cfg := config.Config{Order: []string{"store"}, Backend: map[string]config.BackendSpec{"store": {Type: "file", Path: filepath.Join(dir, "store.age")}}}
	stores := make([]*file.Store, 2)
	for i := range stores {
		bs, e := backend.Build(context.Background(), cfg, backend.Deps{Home: dir, Config: cfg})
		if e != nil || len(bs) != 1 {
			t.Fatal("build failed")
		}
		stores[i] = bs[0].(*file.Store)
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range stores {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = stores[i].SetIfAbsent(context.Background(), "demo", []byte("synthetic"), backend.Meta{})
		}(i)
	}
	wg.Wait()
	winners := 0
	for _, e := range errs {
		if e == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("expected one winner, got %d", winners)
	}
}
