//go:build darwin || linux

package effectgateway

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateIssueStorePublishesOnce(t *testing.T) {
	path := issueStoreTempDir(t)
	ctx := context.Background()
	tokenID := strings.Repeat("a", 32)
	store, err := OpenPrivateIssueStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Lookup(ctx, "first"); err != nil || found {
		t.Fatalf("missing issue record: found=%t error=%v", found, err)
	}
	if err := store.Store(ctx, "first", tokenID, []byte("fixture-value")); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenPrivateIssueStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Store(ctx, "first", strings.Repeat("b", 32), []byte("other-value")); err == nil {
		t.Fatal("existing issued record was replaceable")
	}
	if got, found, err := reopened.Lookup(ctx, "first"); err != nil || !found || got != tokenID {
		t.Fatalf("published issue record: found=%t error=%v", found, err)
	}
	final := filepath.Join(path, "first.issued")
	if info, err := os.Stat(final); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("issued record mode: %v", err)
	}
	if err := os.Symlink(final, filepath.Join(path, "alias.issued")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.Lookup(ctx, "alias"); err == nil {
		t.Fatal("lookup followed a symlink")
	}
	if err := reopened.Store(ctx, "../outside", tokenID, []byte("fixture-value")); err == nil {
		t.Fatal("store accepted traversal")
	}
	if err := reopened.Store(ctx, "oversized", tokenID, make([]byte, issuedValueLimit+1)); err == nil {
		t.Fatal("store accepted an oversized value")
	}
}

func TestPrivateIssueStoreInterruptedPublication(t *testing.T) {
	path := issueStoreTempDir(t)
	ctx := context.Background()
	tokenID := strings.Repeat("c", 32)
	store, err := OpenPrivateIssueStore(path)
	if err != nil {
		t.Fatal(err)
	}
	pending := filepath.Join(path, ".interrupted.issued.pending")
	if err := os.WriteFile(pending, []byte(tokenID+"\nfixture-value"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Lookup(ctx, "interrupted"); !errors.Is(err, ErrIssueStoreUncertain) || found {
		t.Fatalf("unpublished record should remain uncertain: found=%t error=%v", found, err)
	}
	if err := store.Store(ctx, "interrupted", tokenID, []byte("other-value")); err == nil {
		t.Fatal("store replaced an interrupted issue record")
	}

	lock, err := openOperationalDirectory(path, false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if err := store.Store(ctx, "busy", tokenID, []byte("fixture-value")); err == nil {
		t.Fatal("store bypassed the publication lock")
	}
	if _, _, err := store.Lookup(ctx, "busy"); err == nil {
		t.Fatal("lookup bypassed the publication lock")
	}
}

func issueStoreTempDir(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	path, err := os.MkdirTemp(base, "kc-issue-store-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	sink := filepath.Join(path, "issued")
	if err := os.Mkdir(sink, 0700); err != nil {
		t.Fatal(err)
	}
	return sink
}
