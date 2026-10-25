package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/camem2442/keycli/internal/backend"
	_ "github.com/camem2442/keycli/internal/backend/env"
	_ "github.com/camem2442/keycli/internal/backend/external"
	_ "github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/backend/keychain"
	_ "github.com/camem2442/keycli/internal/backend/keychain"
	_ "github.com/camem2442/keycli/internal/backend/link"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/resolve"
	"github.com/camem2442/keycli/internal/testhooks"
)

// App holds wired dependencies for CLI commands.
type App struct {
	Home       string
	Config     config.Config
	ConfigPath string
	Resolver   *resolve.Resolver
	Deps       backend.Deps
}

// New builds the application graph.
func New(ctx context.Context) (*App, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	if h := os.Getenv("HOME"); h != "" {
		home = h
	}
	cfg, cfgPath, err := config.Load(home)
	if err != nil {
		return nil, err
	}
	runner := keychain.ExecRunner{Config: cfg}
	deps := backend.Deps{
		Home:                     home,
		Config:                   cfg,
		SecurityRunner:           runner,
		AllowKeychainOnNonDarwin: testhooks.Env(cfg, "KC_TEST_KEYCHAIN") == "1",
	}
	backends, err := backend.Build(ctx, cfg, deps)
	if err != nil {
		return nil, err
	}
	return &App{
		Home:       home,
		Config:     cfg,
		ConfigPath: cfgPath,
		Resolver:   resolve.New(cfg, backends, home, auditWarn),
		Deps:       deps,
	}, nil
}

func auditWarn(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}

// StorePath returns the default age store path.
func (a *App) StorePath() string {
	if spec, ok := a.Config.Backend["store"]; ok && spec.Path != "" {
		return spec.Path
	}
	return filepath.Join(a.Home, ".kc", "store.age")
}

// SaveConfig persists the in-memory config to ConfigPath.
func (a *App) SaveConfig() error {
	return config.Save(a.ConfigPath, a.Config)
}

// Reload rebuilds backends and resolver after config mutation.
func (a *App) Reload(ctx context.Context) error {
	a.Deps.Config = a.Config
	backends, err := backend.Build(ctx, a.Config, a.Deps)
	if err != nil {
		return err
	}
	a.Resolver = resolve.New(a.Config, backends, a.Home, auditWarn)
	return nil
}
