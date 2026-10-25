package ledger_test

import (
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/camem2442/keycli/internal/ledger"
)

func TestRecordAndStale(t *testing.T) {
	dir := t.TempDir()
	lf := ledger.File{Path: filepath.Join(dir, "ledger.json")}
	key := []byte("hmac-key-32-bytes-long!!!!!!!")
	if err := lf.Record(key, "github", "cursor", []byte("v1")); err != nil {
		t.Fatal(err)
	}
	if !lf.Stale(key, "github", "cursor", []byte("v2")) {
		t.Fatal("expected stale after change")
	}
	if lf.Stale(key, "github", "cursor", []byte("v1")) {
		t.Fatal("expected fresh")
	}
}

func TestRecordAllUpsertsInOneWrite(t *testing.T) {
	dir := t.TempDir()
	lf := ledger.File{Path: filepath.Join(dir, "ledger.json")}
	key := []byte("hmac-key-32-bytes-long!!!!!!!")
	items := []ledger.RecordItem{
		{Name: "A", Target: "cursor", Value: []byte("v1")},
		{Name: "b", Target: "cursor", Value: []byte("v2")},
		{Name: "a", Target: "gh", Value: []byte("v3")},
	}
	if err := lf.RecordAll(key, items); err != nil {
		t.Fatal(err)
	}
	entries, err := lf.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if err := lf.RecordAll(key, items); err != nil {
		t.Fatal(err)
	}
	entries, err = lf.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("upsert duplicated entries: %d", len(entries))
	}
	if lf.Stale(key, "a", "cursor", []byte("v1")) {
		t.Fatal("batch record should store the current value")
	}
	if !lf.Stale(key, "a", "cursor", []byte("changed")) {
		t.Fatal("expected stale after value change")
	}
}

func TestRecordAllEmptyNoop(t *testing.T) {
	lf := ledger.File{Path: filepath.Join(t.TempDir(), "ledger.json")}
	if err := lf.RecordAll(nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := lf.RecordAll(nil, []ledger.RecordItem{{Name: "a"}}); err == nil {
		t.Fatal("empty key must be rejected when items are present")
	}
}

func TestConcurrentRecordKeepsAllEntries(t *testing.T) {
	dir := t.TempDir()
	lf := ledger.File{Path: filepath.Join(dir, "ledger.json")}
	key := []byte("hmac-key-32-bytes-long!!!!!!!")
	const n = 12
	errc := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errc <- lf.Record(key, "name-"+strconv.Itoa(i), "cursor", []byte("v"))
		}(i)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, err := lf.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != n {
		t.Fatalf("lost ledger entries: got %d want %d", len(entries), n)
	}
}
