package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
	_ "github.com/camem2442/keycli/internal/backend/env"
	_ "github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/resolve"
)

func newTestApp(t *testing.T) *app.App {
	t.Helper()
	t.Setenv("GH_TOKEN", "ghp_test12345")
	t.Setenv("OPENAI_API_KEY", "sk-test67890")
	t.Setenv("INTERNAL_SECRET", "secret_val")

	dir := t.TempDir()
	cfg := config.Default(dir)
	cfg.Alias = map[string][]string{
		"github": {"gh", "gh_token"},
	}

	backends, err := backend.Build(context.Background(), cfg, backend.Deps{Home: dir, Config: cfg})
	if err != nil {
		t.Fatal(err)
	}
	r := resolve.New(cfg, backends, dir, func(string) {})

	return &app.App{
		Config:   cfg,
		Resolver: r,
		Home:     dir,
	}
}

func TestMCPServerLifecycle(t *testing.T) {
	a := newTestApp(t)

	// Prepare sequence of requests
	requests := []string{
		// 1. Initialize
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		// 2. Initialized notification
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		// 3. Ping
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		// 4. Tools list
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		// 5. Tool call: list_secrets
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"list_secrets","arguments":{}}}`,
		// 6. Tool call: get_secret (alias 'gh')
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"get_secret","arguments":{"name":"gh"}}}`,
		// 7. Tool call: get_secret (nonexistent)
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"get_secret","arguments":{"name":"does_not_exist"}}}`,
		// 8. Tool call: policy_check
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"policy_check","arguments":{"name":"github"}}}`,
		// 9. Tool call: doctor
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"doctor","arguments":{}}}`,
		// 10. Unknown method
		`{"jsonrpc":"2.0","id":9,"method":"unknown_method"}`,
	}

	input := strings.Join(requests, "\n") + "\n"
	in := strings.NewReader(input)
	out := &bytes.Buffer{}

	server := NewServer(a, in, out)
	if err := server.Run(context.Background()); err != nil {
		t.Fatalf("Server.Run failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 9 { // initialized notification does not produce a response
		t.Fatalf("expected 9 responses, got %d: %v", len(lines), lines)
	}

	// 1. Check initialize
	var resp1 Response
	if err := json.Unmarshal([]byte(lines[0]), &resp1); err != nil {
		t.Fatalf("failed to parse resp1: %v", err)
	}
	if resp1.Error != nil {
		t.Fatalf("resp1 returned error: %v", resp1.Error)
	}

	// 2. Check ping
	var resp2 Response
	if err := json.Unmarshal([]byte(lines[1]), &resp2); err != nil {
		t.Fatalf("failed to parse resp2: %v", err)
	}

	// 3. Check tools/list
	var resp3 Response
	if err := json.Unmarshal([]byte(lines[2]), &resp3); err != nil {
		t.Fatalf("failed to parse resp3: %v", err)
	}
	res3Bytes, _ := json.Marshal(resp3.Result)
	var listRes ListToolsResult
	_ = json.Unmarshal(res3Bytes, &listRes)
	if len(listRes.Tools) < 4 {
		t.Errorf("expected at least 4 tools, got %d", len(listRes.Tools))
	}

	// 4. Check list_secrets
	var resp4 Response
	if err := json.Unmarshal([]byte(lines[3]), &resp4); err != nil {
		t.Fatalf("failed to parse resp4: %v", err)
	}
	res4Bytes, _ := json.Marshal(resp4.Result)
	var callRes4 CallToolResult
	_ = json.Unmarshal(res4Bytes, &callRes4)
	if callRes4.IsError || len(callRes4.Content) == 0 {
		t.Fatalf("list_secrets failed: %v", callRes4)
	}

	// 5. Check get_secret 'gh' -> 'ghp_test12345'
	var resp5 Response
	if err := json.Unmarshal([]byte(lines[4]), &resp5); err != nil {
		t.Fatalf("failed to parse resp5: %v", err)
	}
	res5Bytes, _ := json.Marshal(resp5.Result)
	var callRes5 CallToolResult
	_ = json.Unmarshal(res5Bytes, &callRes5)
	if callRes5.IsError || len(callRes5.Content) == 0 || callRes5.Content[0].Text != "ghp_test12345" {
		t.Fatalf("expected 'ghp_test12345', got %v", callRes5)
	}

	// 6. Check get_secret nonexistent
	var resp6 Response
	if err := json.Unmarshal([]byte(lines[5]), &resp6); err != nil {
		t.Fatalf("failed to parse resp6: %v", err)
	}
	res6Bytes, _ := json.Marshal(resp6.Result)
	var callRes6 CallToolResult
	_ = json.Unmarshal(res6Bytes, &callRes6)
	if !callRes6.IsError {
		t.Fatalf("expected error for nonexistent secret, got: %v", callRes6)
	}

	// 7. Check policy_check
	var resp7 Response
	if err := json.Unmarshal([]byte(lines[6]), &resp7); err != nil {
		t.Fatalf("failed to parse resp7: %v", err)
	}
	res7Bytes, _ := json.Marshal(resp7.Result)
	var callRes7 CallToolResult
	_ = json.Unmarshal(res7Bytes, &callRes7)
	if callRes7.IsError {
		t.Fatalf("policy_check returned error: %v", callRes7)
	}

	// 8. Check doctor
	var resp8 Response
	if err := json.Unmarshal([]byte(lines[7]), &resp8); err != nil {
		t.Fatalf("failed to parse resp8: %v", err)
	}
	res8Bytes, _ := json.Marshal(resp8.Result)
	var callRes8 CallToolResult
	_ = json.Unmarshal(res8Bytes, &callRes8)
	if callRes8.IsError || len(callRes8.Content) == 0 {
		t.Fatalf("doctor returned error: %v", callRes8)
	}

	// 9. Check unknown method
	var resp9 Response
	if err := json.Unmarshal([]byte(lines[8]), &resp9); err != nil {
		t.Fatalf("failed to parse resp9: %v", err)
	}
	if resp9.Error == nil || resp9.Error.Code != ErrCodeMethodNotFound {
		t.Fatalf("expected MethodNotFound error, got: %v", resp9)
	}
}

func TestMCPParseError(t *testing.T) {
	a := newTestApp(t)
	in := strings.NewReader("not a valid json\n")
	out := &bytes.Buffer{}

	server := NewServer(a, in, out)
	_ = server.Run(context.Background())

	var resp Response
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != ErrCodeParseError {
		t.Fatalf("expected parse error, got: %v", resp)
	}
}
