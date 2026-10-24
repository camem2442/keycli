package backend

import (
	"context"
	"fmt"
	"sync"

	"github.com/camem2442/keycli/internal/config"
)

var (
	mu        sync.RWMutex
	factories = map[string]Factory{}
)

// Register adds a backend factory for a type string (called from init()).
func Register(kind string, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[kind] = f
}

// Build instantiates backends from config in order.
func Build(ctx context.Context, cfg config.Config, deps Deps) ([]Backend, error) {
	mu.RLock()
	defer mu.RUnlock()
	var out []Backend
	for _, name := range cfg.Order {
		spec, ok := cfg.Backend[name]
		if !ok {
			continue
		}
		if spec.Type == "external" && spec.Enabled != nil && !*spec.Enabled {
			continue
		}
		f, ok := factories[spec.Type]
		if !ok {
			return nil, fmt.Errorf("unknown backend type %q", spec.Type)
		}
		b, err := f(ctx, name, BackendBuildArgs{Spec: spec, Deps: deps})
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

// BackendBuildArgs passes spec and shared dependencies to factories.
type BackendBuildArgs struct {
	Spec config.BackendSpec
	Deps Deps
}

// Deps are injectable dependencies for backends and tests.
type Deps struct {
	Home                     string
	Config                   config.Config
	SecurityRunner           SecurityRunner // keychain only; nil => default exec
	PluginRunner             interface{}    // external.PluginRunner when set (tests)
	NetworkBlocked           func() bool    // skip NeedsNetwork backends when true
	AllowKeychainOnNonDarwin bool           // tests only (fake security runner)
}

// SecurityRunner runs /usr/bin/security (injectable in tests).
type SecurityRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
	RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error)
}
