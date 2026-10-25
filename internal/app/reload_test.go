package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/resolve"
)

type fakeSecurity struct {
	value string
}

func (f fakeSecurity) Run(ctx context.Context, args ...string) ([]byte, error) {
	if f.value == "" {
		return nil, fmt.Errorf("not found")
	}
	return []byte(f.value), nil
}

func (f fakeSecurity) RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	return nil, nil
}

// TestReloadUsesNewConfig covers P1-8: backends rebuilt by Reload must see the
// mutated in-memory config, not the stale Deps.Config snapshot.
func TestReloadUsesNewConfig(t *testing.T) {
	home := t.TempDir()
	cfg := config.Config{
		Order:   []string{"link"},
		Backend: map[string]config.BackendSpec{"link": {Type: "link"}},
		Link:    map[string]config.LinkTarget{},
	}
	deps := backend.Deps{Home: home, Config: cfg, SecurityRunner: fakeSecurity{value: "linked-value"}}
	backends, err := backend.Build(context.Background(), cfg, deps)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{
		Home:     home,
		Config:   cfg,
		Resolver: resolve.New(cfg, backends, home, nil),
		Deps:     deps,
	}

	a.Config.Link["ext"] = config.LinkTarget{Service: "svc", Account: "acct"}
	config.EnsureLinkBackend(&a.Config)
	if err := a.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	val, _, err := a.Resolver.Get(context.Background(), "ext")
	if err != nil || string(val) != "linked-value" {
		t.Fatalf("Reload did not pick up the new link: %q %v", val, err)
	}
}
