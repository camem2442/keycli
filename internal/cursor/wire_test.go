package cursor

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCleanAndValidateBaseURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "https://example.com/v1",
			expected: "https://example.com/v1",
		},
		{
			input:    "https://example.com",
			expected: "https://example.com/v1",
		},
		{
			input:    "  'https://rabbit-tar-fathers- requirement.trycloudflare.com/v1'  ",
			expected: "https://rabbit-tar-fathers-requirement.trycloudflare.com/v1",
		},
		{
			input:    "https://tunnel.trycloudflare.com # inline comment",
			expected: "https://tunnel.trycloudflare.com/v1",
		},
		{
			input:    "http://127.0.0.1:8787/v1/",
			expected: "http://127.0.0.1:8787/v1",
		},
		{
			input:    "ftp://invalid-scheme.com",
			expected: "",
		},
		{
			input:    "   ",
			expected: "",
		},
	}

	for _, tc := range tests {
		actual := CleanAndValidateBaseURL(tc.input)
		if actual != tc.expected {
			t.Errorf("CleanAndValidateBaseURL(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestActiveTunnelBaseURL(t *testing.T) {
	dir := t.TempDir()
	proxyDir := filepath.Join(dir, ".config", "cursor-vertex-proxy")
	if err := os.MkdirAll(proxyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Missing both files
	if _, err := ActiveTunnelBaseURL(dir); err == nil {
		t.Fatal("expected error when no proxy files exist")
	}

	// 2. config.env fallback
	cfgContent := "PROXY_PORT=8787\nLAST_TUNNEL_BASE_URL=https://fallback-tunnel.trycloudflare.com/v1\n"
	if err := os.WriteFile(filepath.Join(proxyDir, "config.env"), []byte(cfgContent), 0o600); err != nil {
		t.Fatal(err)
	}
	url, err := ActiveTunnelBaseURL(dir)
	if err != nil || url != "https://fallback-tunnel.trycloudflare.com/v1" {
		t.Fatalf("config.env fallback failed: url=%s, err=%v", url, err)
	}

	// 3. current_url.txt takes precedence (and cleans space)
	urlContent := "https://current- spaced.trycloudflare.com \n"
	if err := os.WriteFile(filepath.Join(proxyDir, "current_url.txt"), []byte(urlContent), 0o600); err != nil {
		t.Fatal(err)
	}
	url, err = ActiveTunnelBaseURL(dir)
	if err != nil || url != "https://current-spaced.trycloudflare.com/v1" {
		t.Fatalf("current_url.txt precedence failed: url=%s, err=%v", url, err)
	}
}

func TestWireCursorVertex(t *testing.T) {
	// Skip if sqlite3 CLI is not installed
	if _, err := findSQLiteBin(); err != nil {
		t.Skip("sqlite3 not found, skipping SQLite integration test")
	}

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	// Create Cursor-compatible SQLite schema
	cmd := exec.Command("sqlite3", dbPath, "CREATE TABLE ItemTable (key TEXT UNIQUE ON CONFLICT REPLACE, value BLOB);")
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	// Insert existing configuration with dummy fields
	initial := map[string]any{
		"openAIBaseUrl": "https://old-url.trycloudflare.com/v1",
		"useOpenAIKey":  false,
		"theme":         "vs-dark",
	}
	initialBytes, _ := json.Marshal(initial)
	insertCmd := exec.Command("sqlite3", dbPath, "INSERT INTO ItemTable (key, value) VALUES ('"+ApplicationUserKey+"', '"+string(initialBytes)+"');")
	if err := insertCmd.Run(); err != nil {
		t.Fatalf("failed to insert initial row: %v", err)
	}

	targetURL := "https://new-tunnel.trycloudflare.com/v1"

	// 1. Dry run
	res, err := WireCursorVertex(dir, dbPath, targetURL, true, false)
	if err != nil {
		t.Fatalf("dry run failed: %v", err)
	}
	if res.PreviousURL != "https://old-url.trycloudflare.com/v1" {
		t.Fatalf("expected prevURL 'https://old-url.trycloudflare.com/v1', got %q", res.PreviousURL)
	}
	if res.NewURL != targetURL {
		t.Fatalf("expected newURL %q, got %q", targetURL, res.NewURL)
	}
	if res.Updated {
		t.Fatal("expected res.Updated to be false on dry-run")
	}

	// Verify DB was NOT updated during dry-run
	current, _ := ReadCursorBaseURL(dbPath)
	if current != "https://old-url.trycloudflare.com/v1" {
		t.Fatalf("dry run modified db: %s", current)
	}

	// 2. Real update
	res, err = WireCursorVertex(dir, dbPath, targetURL, false, false)
	if err != nil {
		t.Fatalf("wire update failed: %v", err)
	}
	if !res.Updated {
		t.Fatal("expected res.Updated to be true")
	}

	// Verify DB updated
	current, err = ReadCursorBaseURL(dbPath)
	if err != nil {
		t.Fatalf("failed to read back url: %v", err)
	}
	if current != targetURL {
		t.Fatalf("expected %q, got %q", targetURL, current)
	}

	// Verify other fields preserved
	out, err := exec.Command("sqlite3", dbPath, "SELECT value FROM ItemTable WHERE key = '"+ApplicationUserKey+"';").Output()
	if err != nil {
		t.Fatal(err)
	}
	var updatedDoc map[string]any
	if err := json.Unmarshal(out, &updatedDoc); err != nil {
		t.Fatal(err)
	}
	if updatedDoc["theme"] != "vs-dark" {
		t.Fatalf("existing key 'theme' was overwritten: %v", updatedDoc)
	}
	if updatedDoc["useOpenAIKey"] != true {
		t.Fatalf("useOpenAIKey is not true: %v", updatedDoc["useOpenAIKey"])
	}
}
