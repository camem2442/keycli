package resolve_test

import (
	"context"
	"fmt"
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
	t.Setenv("GITHUB_TOKEN", "")
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

type lockedBackend struct{}

func (lockedBackend) InstanceName() string { return "locked" }
func (lockedBackend) Kind() string         { return "file" }
func (lockedBackend) Caps() backend.Capabilities {
	return backend.Capabilities{CanWrite: true, CanList: true}
}
func (lockedBackend) Available(context.Context) (bool, string, error) {
	return false, "", fmt.Errorf("%w", kcerrors.ErrLocked)
}
func (lockedBackend) Get(context.Context, string) ([]byte, error) {
	return nil, fmt.Errorf("%w", kcerrors.ErrLocked)
}
func (lockedBackend) List(context.Context) ([]backend.Item, error) {
	return nil, fmt.Errorf("%w", kcerrors.ErrLocked)
}
func (lockedBackend) Set(context.Context, string, []byte, backend.Meta) error {
	return fmt.Errorf("%w", kcerrors.ErrLocked)
}
func (lockedBackend) Delete(context.Context, string) error {
	return fmt.Errorf("%w", kcerrors.ErrLocked)
}

// TestListPropagatesLocked covers P0-3: a locked store must not look like an
// empty store.
func TestListPropagatesLocked(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	r := resolve.New(cfg, []backend.Backend{lockedBackend{}}, dir, nil)
	items, err := r.List(context.Background())
	if kcerrors.Code(err) != kcerrors.ExitLocked {
		t.Fatalf("code %d err %v items=%d", kcerrors.Code(err), err, len(items))
	}
}

type networkBackend struct {
	calls int
}

func (b *networkBackend) InstanceName() string { return "remote" }
func (b *networkBackend) Kind() string         { return "external" }
func (b *networkBackend) Caps() backend.Capabilities {
	return backend.Capabilities{CanList: true, NeedsNetwork: true}
}
func (b *networkBackend) Available(context.Context) (bool, string, error) {
	return true, "", nil
}
func (b *networkBackend) Get(context.Context, string) ([]byte, error) {
	b.calls++
	return []byte("remote-value"), nil
}
func (b *networkBackend) List(context.Context) ([]backend.Item, error) {
	b.calls++
	return []backend.Item{{Name: "remote"}}, nil
}
func (b *networkBackend) Set(context.Context, string, []byte, backend.Meta) error {
	b.calls++
	return nil
}
func (b *networkBackend) Delete(context.Context, string) error {
	b.calls++
	return nil
}

// TestReadPathsSkipNetworkBackends covers P0-5: listing/reading values must not
// contact network-capable backends.
func TestReadPathsSkipNetworkBackends(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	remote := &networkBackend{}
	r := resolve.New(cfg, []backend.Backend{remote}, dir, nil)
	items, err := r.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("network backend contributed to ls: %#v", items)
	}
	_, _, err = r.Get(context.Background(), "remote")
	if kcerrors.Code(err) != kcerrors.ExitNotFound {
		t.Fatalf("get: %v", err)
	}
	if remote.calls != 0 {
		t.Fatalf("network backend was contacted %d times", remote.calls)
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
