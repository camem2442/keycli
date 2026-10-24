package middleware

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// DetectAgent returns a stable agent profile for policy and audit (never includes secrets).
// KC_AGENT overrides automatic detection.
func DetectAgent() string {
	if v := strings.TrimSpace(os.Getenv("KC_AGENT")); v != "" {
		return normalizeAgent(v)
	}
	if truthy(os.Getenv("CURSOR_AGENT")) || os.Getenv("CURSOR_TRACE_ID") != "" {
		return "cursor"
	}
	if truthy(os.Getenv("CLAUDE_CODE")) || os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return "claude"
	}
	if truthy(os.Getenv("CODEX_SANDBOX")) || os.Getenv("CODEX_INTERNAL_ORIGINATOR_OVERRIDE") != "" {
		return "codex"
	}
	if truthy(os.Getenv("CI")) || truthy(os.Getenv("GITHUB_ACTIONS")) || truthy(os.Getenv("GITLAB_CI")) {
		return "ci"
	}
	if isHumanTTY() {
		return "human"
	}
	return "unknown"
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
