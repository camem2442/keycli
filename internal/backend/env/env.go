package env

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/kcerrors"
)

func init() {
	backend.Register("env", func(ctx context.Context, instanceName string, spec any) (backend.Backend, error) {
		args, ok := spec.(backend.BackendBuildArgs)
		if !ok {
			return nil, fmt.Errorf("env: invalid build args")
		}
		return &Env{
			instance: instanceName,
			alias:    args.Deps.Config.Alias,
		}, nil
	})
}

// Env reads process environment (read-only).
type Env struct {
	instance string
	alias    map[string][]string
}

func (e *Env) InstanceName() string { return e.instance }
func (e *Env) Kind() string         { return "env" }
func (e *Env) Caps() backend.Capabilities {
	return backend.Capabilities{CanList: true}
}

func (e *Env) Available(ctx context.Context) (bool, string, error) {
	return true, "", nil
}

func (e *Env) Get(ctx context.Context, name string) ([]byte, error) {
	val, _, err := e.GetWithSource(ctx, name)
	return val, err
}

// GetWithSource returns the value and env var name used.
func (e *Env) GetWithSource(ctx context.Context, name string) ([]byte, string, error) {
	for _, k := range envKeysForName(name) {
		if v := os.Getenv(k); v != "" {
			return []byte(v), "env:" + k, nil
		}
	}
	return nil, "", fmt.Errorf("%w", kcerrors.ErrNotFound)
}

func envKeysForName(name string) []string {
	n := strings.TrimSpace(name)
	upper := strings.ToUpper(strings.ReplaceAll(n, "-", "_"))
	return []string{upper, n}
}

func (e *Env) List(ctx context.Context) ([]backend.Item, error) {
	var items []backend.Item
	seen := map[string]struct{}{}
	for canon, als := range e.alias {
		names := append([]string{canon}, als...)
		for _, n := range names {
			for _, k := range envKeysForName(n) {
				if v := os.Getenv(k); v != "" {
					if _, ok := seen[strings.ToLower(canon)]; ok {
						goto nextCanon
					}
					seen[strings.ToLower(canon)] = struct{}{}
					items = append(items, backend.Item{Name: canon})
					goto nextCanon
				}
			}
		}
	nextCanon:
	}
	// Do not synthesize names from KC_* variables: they are kc internals
	// (KC_KEY, KC_AGENT, ...), not secrets, and none of them is readable by
	// name. Listing them made ls show entries that get could not resolve.
	return items, nil
}

func (e *Env) Set(ctx context.Context, name string, value []byte, meta backend.Meta) error {
	return fmt.Errorf("env backend is read-only")
}

func (e *Env) Delete(ctx context.Context, name string) error {
	return fmt.Errorf("env backend is read-only")
}
