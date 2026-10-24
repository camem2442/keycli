package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/camem2442/keycli/internal/backend"
	_ "github.com/camem2442/keycli/internal/backend/env"
	_ "github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/backend/keychain"
	_ "github.com/camem2442/keycli/internal/backend/keychain"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/resolve"
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
	runner := keychain.ExecRunner{}
	deps := backend.Deps{
		Home:                     home,
		Config:                   cfg,
		SecurityRunner:           runner,
		AllowKeychainOnNonDarwin: os.Getenv("KC_TEST_KEYCHAIN") == "1",
	}
	backends, err := backend.Build(ctx, cfg, deps)
	if err != nil {
		return nil, err
	}
	return &App{
		Home:       home,
		Config:     cfg,
		ConfigPath: cfgPath,
		Resolver:   resolve.New(cfg, backends),
		Deps:       deps,
	}, nil
}

// StorePath returns the default age store path.
func (a *App) StorePath() string {
	if spec, ok := a.Config.Backend["store"]; ok && spec.Path != "" {
		return spec.Path
	}
	return filepath.Join(a.Home, ".kc", "store.age")
}
