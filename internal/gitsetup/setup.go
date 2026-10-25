package gitsetup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/camem2442/keycli/internal/config"
)

// DefaultHost is the host configured when --host is omitted.
const DefaultHost = "github.com"

// HelperKey returns the git config key that binds a credential helper to host.
func HelperKey(host string) string {
	return fmt.Sprintf("credential.https://%s.helper", config.NormalizeGitHost(host))
}

// Plan describes setup-git changes without applying them.
type Plan struct {
	KcPath      string
	Host        string
	Key         string
	HelperValue string
	Current     string
	GhSnippet   string
}

// BuildPlan returns the git helper configuration to apply for host.
func BuildPlan(kcPath, host string) Plan {
	kcPath = strings.TrimSpace(kcPath)
	if strings.TrimSpace(host) == "" {
		host = DefaultHost
	}
	host = config.NormalizeGitHost(host)
	return Plan{
		KcPath:      kcPath,
		Host:        host,
		Key:         HelperKey(host),
		HelperValue: fmt.Sprintf("!%s git-credential", kcPath),
		GhSnippet:   GhWrapperSnippet(),
	}
}

// GhWrapperSnippet is the recommended low-friction gh integration (no token in shell files).
func GhWrapperSnippet() string {
	return `# GitHub CLI via kc (no token in gitconfig or shell history):
gh() { command kc exec --only github -- command gh "$@"; }`
}

// ReadCurrentHelper returns the configured helper for host (empty when unset).
func ReadCurrentHelper(host string) (string, error) {
	out, err := exec.Command("git", "config", "--global", "--get", HelperKey(host)).Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}

// Apply configures git to use kc git-credential for the plan's host.
//
// An empty helper entry is written first: git treats it as "reset the helper
// list", which stops global helpers (e.g. osxkeychain) from also handling this
// host. kc is authoritative for hosts it is configured for.
func Apply(plan Plan) error {
	key := plan.Key
	if key == "" {
		key = HelperKey(plan.Host)
	}
	_ = exec.Command("git", "config", "--global", "--unset-all", key).Run()
	if err := exec.Command("git", "config", "--global", "--add", key, "").Run(); err != nil {
		return err
	}
	return exec.Command("git", "config", "--global", "--add", key, plan.HelperValue).Run()
}

// Undo removes every kc git credential helper value for host if kc owns it.
func Undo(kcPath, host string) error {
	key := HelperKey(host)
	cur, _ := ReadCurrentHelper(host)
	want := fmt.Sprintf("!%s git-credential", kcPath)
	if cur != want && !strings.Contains(cur, "kc git-credential") {
		return fmt.Errorf("git helper is not managed by kc: %q", cur)
	}
	return exec.Command("git", "config", "--global", "--unset-all", key).Run()
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
