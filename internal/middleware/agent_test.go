package middleware_test

import (
	"testing"

	"github.com/camem2442/keycli/internal/middleware"
)

func TestDetectAgentKCAgentOverride(t *testing.T) {
	t.Setenv("KC_AGENT", "custom")
	t.Setenv("CURSOR_AGENT", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess")
	if middleware.DetectAgent() != "custom" {
		t.Fatal("KC_AGENT must win")
	}
}

func TestDetectAgentClaudeBeforeCursor(t *testing.T) {
	t.Setenv("KC_AGENT", "")
	t.Setenv("CURSOR_AGENT", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "abc")
	if middleware.DetectAgent() != "claude" {
		t.Fatalf("got %q", middleware.DetectAgent())
	}
}

func TestDetectAgentClaudeCodeEntrypointWithCursor(t *testing.T) {
	t.Setenv("KC_AGENT", "")
	t.Setenv("CURSOR_AGENT", "true")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	if middleware.DetectAgent() != "claude" {
		t.Fatalf("got %q", middleware.DetectAgent())
	}
}

func TestDetectAgentCodexBeforeCursor(t *testing.T) {
	t.Setenv("KC_AGENT", "")
	t.Setenv("CURSOR_AGENT", "1")
	t.Setenv("CODEX_SESSION_ID", "sid")
	if middleware.DetectAgent() != "codex" {
		t.Fatalf("got %q", middleware.DetectAgent())
	}
}

func TestDetectAgentCodexThreadWithCursor(t *testing.T) {
	t.Setenv("KC_AGENT", "")
	t.Setenv("CURSOR_AGENT", "1")
	t.Setenv("CODEX_THREAD_ID", "tid")
	if middleware.DetectAgent() != "codex" {
		t.Fatalf("got %q", middleware.DetectAgent())
	}
}

func TestDetectAgentCodexCINotGenericCI(t *testing.T) {
	t.Setenv("KC_AGENT", "")
	t.Setenv("CI", "true")
	t.Setenv("CODEX_CI", "1")
	if middleware.DetectAgent() != "codex" {
		t.Fatalf("CODEX_CI should map to codex, got %q", middleware.DetectAgent())
	}
}

func TestDetectAgentCursorOnly(t *testing.T) {
	t.Setenv("KC_AGENT", "")
	t.Setenv("CURSOR_AGENT", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("CODEX_SESSION_ID", "")
	if middleware.DetectAgent() != "cursor" {
		t.Fatalf("got %q", middleware.DetectAgent())
	}
}

func TestDetectAgentGenericCI(t *testing.T) {
	t.Setenv("KC_AGENT", "")
	t.Setenv("CURSOR_AGENT", "")
	t.Setenv("GITHUB_ACTIONS", "true")
	if middleware.DetectAgent() != "ci" {
		t.Fatalf("got %q", middleware.DetectAgent())
	}
}
