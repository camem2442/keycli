package gitsetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const gitHelperKey = "credential.https://github.com.helper"

// Plan describes setup-git changes without applying them.
type Plan struct {
	KcPath      string
	HelperValue string
	Current     string
	GhSnippet   string
}

// BuildPlan returns the git helper configuration to apply.
func BuildPlan(kcPath string) Plan {
	kcPath = strings.TrimSpace(kcPath)
	helper := fmt.Sprintf("!%s git-credential", kcPath)
	return Plan{
		KcPath:      kcPath,
		HelperValue: helper,
		GhSnippet:   GhWrapperSnippet(),
	}
}

// GhWrapperSnippet is the recommended low-friction gh integration (no token in shell files).
func GhWrapperSnippet() string {
	return `# GitHub CLI via kc (no token in gitconfig or shell history):
gh() { command kc exec --only github -- command gh "$@"; }`
}

// ReadCurrentHelper runs git config --global --get.
func ReadCurrentHelper() (string, error) {
	out, err := exec.Command("git", "config", "--global", "--get", gitHelperKey).Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// Apply configures git to use kc git-credential for github.com.
func Apply(plan Plan) error {
	return exec.Command("git", "config", "--global", gitHelperKey, plan.HelperValue).Run()
}

// Undo removes the kc git credential helper if it matches.
func Undo(kcPath string) error {
	cur, _ := ReadCurrentHelper()
	want := fmt.Sprintf("!%s git-credential", kcPath)
	if cur != want && !strings.Contains(cur, "kc git-credential") {
		return fmt.Errorf("git helper is not managed by kc: %q", cur)
	}
	return exec.Command("git", "config", "--global", "--unset", gitHelperKey).Run()
}

// ResolveKCPath finds the kc binary path for git helper configuration.
func ResolveKCPath() (string, error) {
	if p := os.Getenv("KC_BIN"); p != "" {
		return p, nil
	}
	ex, err := os.Executable()
	if err != nil {
		return "", err
	}
	// If invoked via symlink keycli, still fine.
	if base := filepath.Base(ex); base == "kc" || base == "keycli" {
		return ex, nil
	}
	if p, err := exec.LookPath("kc"); err == nil {
		return p, nil
	}
	return ex, nil
}
