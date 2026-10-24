package keychain_test

import (
	"context"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/keychain"
	"github.com/camem2442/keycli/internal/config"

	_ "github.com/camem2442/keycli/internal/backend/keychain"
)

func TestKeychainFakeReadWrite(t *testing.T) {
	fake := &keychain.FakeRunner{Secrets: map[string]string{
		"local.study.ai.groq": "readonly-val",
		"local.study.ai.demo": "demo-val",
	}}
	cfg := config.Config{
		Order: []string{"work"},
		Backend: map[string]config.BackendSpec{
			"work": {Type: "keychain", ServicePrefix: "local.study.ai"},
		},
	}
	deps := backend.Deps{
		Home:                     t.TempDir(),
		Config:                   cfg,
		SecurityRunner:           fake,
		AllowKeychainOnNonDarwin: true,
	}
	// Force available on Linux by using fake + keychain type; Available checks GOOS.
	// Build still works; Get on Linux returns blocked without darwin.
	backends, err := backend.Build(context.Background(), cfg, deps)
	if err != nil {
		t.Fatal(err)
	}
	kb := backends[0]
	ctx := context.Background()
	val, err := kb.Get(ctx, "groq")
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "readonly-val" {
		t.Fatalf("got %q", val)
	}
	err = kb.Set(ctx, "groq", []byte("x"), backend.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if fake.Secrets["local.study.ai.groq"] != "readonly-val" {
		t.Fatal("must not overwrite ai-core item")
	}
	if fake.Secrets["kc.secret.groq"] != "x" {
		t.Fatal("expected separate write namespace")
	}
	err = kb.Set(ctx, "writable", []byte("secret"), backend.Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if fake.Secrets["kc.secret.writable"] != "secret" {
		t.Fatal("expected write via stdin runner")
	}
}
