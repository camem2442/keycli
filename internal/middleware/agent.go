package middleware

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// DetectAgent returns a stable agent profile for policy and audit (never includes secrets).
// KC_AGENT overrides automatic detection.
// More specific agents are checked before inherited parent markers (e.g. CURSOR_AGENT).
func DetectAgent() string {
	if v := strings.TrimSpace(os.Getenv("KC_AGENT")); v != "" {
		return normalizeAgent(v)
	}
	if detectClaude() {
		return "claude"
	}
	if detectCodex() {
		return "codex"
	}
	if detectCursor() {
		return "cursor"
	}
	if detectCI() {
		return "ci"
	}
	if isHumanTTY() {
		return "human"
	}
	return "unknown"
}

func detectClaude() bool {
	if truthy(os.Getenv("CLAUDECODE")) || truthy(os.Getenv("CLAUDE_CODE")) {
		return true
	}
	if os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return true
	}
	if os.Getenv("CLAUDE_CODE_SESSION_ID") != "" {
		return true
	}
	return false
}

func detectCodex() bool {
	if os.Getenv("CODEX_SESSION_ID") != "" {
		return true
	}
	if os.Getenv("CODEX_THREAD_ID") != "" {
		return true
	}
	if truthy(os.Getenv("CODEX_CI")) {
		return true
	}
	if truthy(os.Getenv("CODEX_SANDBOX")) {
		return true
	}
	if os.Getenv("CODEX_INTERNAL_ORIGINATOR_OVERRIDE") != "" {
		return true
	}
	return false
}

func detectCursor() bool {
	return truthy(os.Getenv("CURSOR_AGENT")) || os.Getenv("CURSOR_TRACE_ID") != ""
}

func detectCI() bool {
	return truthy(os.Getenv("CI")) || truthy(os.Getenv("GITHUB_ACTIONS")) || truthy(os.Getenv("GITLAB_CI"))
}

func normalizeAgent(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isHumanTTY() bool {
	fd := int(os.Stdin.Fd())
	return term.IsTerminal(fd)
}
