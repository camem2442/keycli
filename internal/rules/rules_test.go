package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := WriteFile(path, TargetAgents); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), beginMarker) {
		t.Fatal("missing begin")
	}
	if err := WriteFile(path, TargetAgents); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(path)
	if strings.Count(string(data2), beginMarker) != 1 {
		t.Fatal("duplicate block")
	}
}
