package audit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/middleware/audit"
)

func TestAuditFileModeAndNoSecrets(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".kc", "audit.log")
	falseVal := false
	cfg := config.Default(home)
	cfg.Audit.Path = path
	cfg.Audit.Enabled = &falseVal
	enabled := true
	cfg.Audit.Enabled = &enabled
	w := audit.NewWriter(home, cfg, nil)
	rec := audit.Record{
		TS:      "2026-01-01T00:00:00Z",
		Command: "get",
		Name:    "demo",
		Source:  "file:store",
		Outcome: "ok",
		Agent:   "test",
	}
	if err := w.Append(rec); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-value") {
		t.Fatal("log must not contain values")
	}
	var parsed audit.Record
	if err := json.Unmarshal(data[:len(data)-1], &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Name != "demo" || parsed.Outcome != "ok" {
		t.Fatalf("record %+v", parsed)
	}
}

func TestAuditRotation(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "audit.log")
	cfg := config.Default(home)
	cfg.Audit.Path = path
	cfg.Audit.MaxBytes = 50
	w := audit.NewWriter(home, cfg, nil)
	big := strings.Repeat("x", 80)
	rec := audit.Record{TS: "t", Command: "get", Name: big, Outcome: "ok", Agent: "a"}
	if err := w.Append(rec); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(audit.Record{TS: "t2", Command: "get", Name: "n", Outcome: "ok", Agent: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotation: %v", err)
	}
}
