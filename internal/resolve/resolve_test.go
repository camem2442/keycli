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

// metadataBackend is a writable backend with (or without) metadata support.
// Its List fails to prove that callers which cannot hold metadata never list.
type metadataBackend struct {
	hasMetadata bool
	listCalls   int
}

func (b *metadataBackend) InstanceName() string { return "meta" }
func (b *metadataBackend) Kind() string         { return "file" }
func (b *metadataBackend) Caps() backend.Capabilities {
	return backend.Capabilities{CanWrite: true, CanList: true, HasMetadata: b.hasMetadata}
}
func (b *metadataBackend) Available(context.Context) (bool, string, error) {
	return true, "", nil
}
func (b *metadataBackend) Get(context.Context, string) ([]byte, error) { return []byte("v"), nil }
func (b *metadataBackend) List(context.Context) ([]backend.Item, error) {
	b.listCalls++
	if b.hasMetadata {
		return []backend.Item{{Name: "demo", Kind: "login", URL: "https://example.test/"}}, nil
	}
	return nil, fmt.Errorf("%w", kcerrors.ErrLocked)
}
func (b *metadataBackend) Set(context.Context, string, []byte, backend.Meta) error { return nil }
func (b *metadataBackend) Delete(context.Context, string) error                    { return nil }

// TestStoredItemsSkipsMetadataLessBackends covers the keychain case: a backend
// that cannot store Kind/URL/Username must not be enumerated, so a plain `kc
// set` keeps working even when enumeration would fail (locked keychain).
func TestStoredItemsSkipsMetadataLessBackends(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	b := &metadataBackend{hasMetadata: false}
	r := resolve.New(cfg, []backend.Backend{b}, dir, nil)
	items, err := r.StoredItems(context.Background())
	if err != nil {
		t.Fatalf("metadata-less backend failed the write path: %v", err)
	}
	if len(items) != 0 || b.listCalls != 0 {
		t.Fatalf("listed a metadata-less backend: items=%d calls=%d", len(items), b.listCalls)
	}
	if _, ok, err := r.StoredItem(context.Background(), "demo"); err != nil || ok {
		t.Fatalf("StoredItem: ok=%v err=%v", ok, err)
	}
}

// TestStoredItemsPropagatesStoreErrors covers the file store: if its
// enumeration fails, the caller must see it instead of erasing metadata.
func TestStoredItemsPropagatesStoreErrors(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	b := &metadataBackend{hasMetadata: true}
	r := resolve.New(cfg, []backend.Backend{b}, dir, nil)
	items, err := r.StoredItems(context.Background())
	if err != nil {
		t.Fatalf("metadata backend must be readable here: %v", err)
	}
	if got := items["demo"]; got.Kind != "login" || got.URL != "https://example.test/" {
		t.Fatalf("metadata lost: %+v", got)
	}
}

// TestStoredItemUsesWriteBackendNotShadow keeps the env-shadow fix pinned:
// the first listable backend may be read-only and hold no metadata.
func TestStoredItemUsesWriteBackendNotShadow(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Default(dir)
	shadow := &metadataBackend{hasMetadata: false} // read-only in practice
	writable := &metadataBackend{hasMetadata: true}
	// Make the shadow read-only so Set must write to the second backend.
	r := resolve.New(cfg, []backend.Backend{readOnly{shadow}, writable}, dir, nil)
	it, ok, err := r.StoredItem(context.Background(), "demo")
	if err != nil || !ok {
		t.Fatalf("StoredItem ok=%v err=%v", ok, err)
	}
	if it.Kind != "login" {
		t.Fatalf("read metadata from the shadow: %+v", it)
	}
}

type readOnly struct{ b *metadataBackend }

func (r readOnly) InstanceName() string       { return r.b.InstanceName() }
func (r readOnly) Kind() string               { return r.b.Kind() }
func (r readOnly) Caps() backend.Capabilities { return backend.Capabilities{CanList: true} }
func (r readOnly) Available(context.Context) (bool, string, error) {
	return true, "", nil
}
func (r readOnly) Get(ctx context.Context, name string) ([]byte, error) { return r.b.Get(ctx, name) }
func (r readOnly) List(ctx context.Context) ([]backend.Item, error)     { return r.b.List(ctx) }
func (r readOnly) Set(context.Context, string, []byte, backend.Meta) error {
	return fmt.Errorf("read-only")
}
func (r readOnly) Delete(context.Context, string) error { return fmt.Errorf("read-only") }
