package ledger_test

import (
	"path/filepath"
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
