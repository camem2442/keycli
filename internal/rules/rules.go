package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const beginMarker = "<!-- kc-rules-begin -->"
const endMarker = "<!-- kc-rules-end -->"

// Target selects agent-specific wording.
type Target string

const (
	TargetCursor Target = "cursor"
	TargetClaude Target = "claude"
	TargetCodex  Target = "codex"
	TargetAgents Target = "agents"
)

// DefaultPath returns the conventional rules file for a target in dir.
func DefaultPath(dir string, t Target) string {
	switch t {
	case TargetCursor:
		return filepath.Join(dir, ".cursor", "rules", "keycli.mdc")
	case TargetClaude:
		return filepath.Join(dir, "CLAUDE.md")
	case TargetCodex:
		return filepath.Join(dir, "AGENTS.md")
	case TargetAgents:
		return filepath.Join(dir, "AGENTS.md")
	default:
		return filepath.Join(dir, "AGENTS.md")
	}
}

// Block returns the instruction text (stable, no secrets).
func Block(t Target) string {
	agent := string(t)
	if t == TargetAgents {
		agent = "coding agents"
	}
	return fmt.Sprintf(`%s
## keycli (kc) secrets

When a token or API key is missing for %s:

1. Run `+"`kc ls --json`"+` to list available secret **names** (never values).
2. Prefer `+"`kc exec --require --only NAME -- COMMAND...`"+` to inject env for a subprocess; otherwise `+"`kc get NAME`"+` (stdout only, do not log).
3. Never echo, paste, or commit secret values.
4. Exit code **3** means sandbox-blocked (keychain/clipboard); ask the user to run outside the sandbox or use env-backed secrets.
5. Exit code **5** means policy denied — ask the user to update the agent allowlist in kc config (`+"`kc policy check NAME`"+`).

Docs: https://github.com/camem2442/keycli/blob/main/docs/contracts/cli.md and https://github.com/camem2442/keycli/blob/main/docs/guides/agents.md
%s`, beginMarker, agent, endMarker)
}

// WriteFile inserts or updates the block in path (idempotent).
func WriteFile(path string, t Target) error {
	block := Block(t)
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	if strings.Contains(content, beginMarker) && strings.Contains(content, endMarker) {
		before, _, ok := strings.Cut(content, beginMarker)
		if !ok {
			return fmt.Errorf("malformed kc rules block")
		}
		_, after, ok := strings.Cut(content, endMarker)
		if !ok {
			return fmt.Errorf("malformed kc rules block")
		}
		content = strings.TrimRight(before, "\n") + "\n\n" + block + strings.TrimLeft(after, "\n")
	} else {
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + block + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
