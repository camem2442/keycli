package file_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/config"

	_ "github.com/camem2442/keycli/internal/backend/file"
)

func TestLedgerHMACKeyPersisted(t *testing.T) {
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
		t.Fatalf("build: %v", err)
	}
	st, ok := backends[0].(*file.Store)
	if !ok {
		t.Fatal("not file store")
	}
	k1, err := st.LedgerHMACKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := st.LedgerHMACKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) != 32 || string(k1) != string(k2) {
		t.Fatal("key not stable")
	}
}
