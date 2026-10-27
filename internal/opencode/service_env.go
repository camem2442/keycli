package opencode

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SyncBackgroundServiceEnv writes apiKey into the OpenCode background service env
// and restarts the service so non-standalone `opencode run` can resolve {env:PROXY_API_KEY}.
func SyncBackgroundServiceEnv(opencodeBin string, apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("opencode service sync: empty %s", EnvAPIKey)
	}
	bin := strings.TrimSpace(opencodeBin)
	if bin == "" {
		bin = "opencode"
	}
	set := exec.Command(bin, "service", "set", "env", EnvAPIKey, apiKey)
	set.Env = serviceCommandEnv()
	if out, err := set.CombinedOutput(); err != nil {
		return fmt.Errorf("opencode service set: %w (%s)", err, trimOutput(out))
	}
	restart := exec.Command(bin, "service", "restart")
	restart.Env = serviceCommandEnv()
	if out, err := restart.CombinedOutput(); err != nil {
		return fmt.Errorf("opencode service restart: %w (%s)", err, trimOutput(out))
	}
	return nil
}

func serviceCommandEnv() []string {
	// Minimal env: keep PATH so homebrew opencode is found when bin is bare "opencode".
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin"
	}
	return []string{
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"LOGNAME=" + os.Getenv("LOGNAME"),
		"PATH=" + path,
	}
}

func trimOutput(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ShouldSyncBackgroundService reports whether kc exec should push PROXY_API_KEY into
// the OpenCode background service before starting opencode.
func ShouldSyncBackgroundService(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	base := filepath.Base(argv[0])
	if base != "opencode" && !strings.HasSuffix(base, "/opencode") {
		return false
	}
	return !hasStandaloneFlag(argv[1:])
}

func hasStandaloneFlag(args []string) bool {
	for _, a := range args {
		if a == "--standalone" || strings.HasPrefix(a, "--standalone=") {
			return true
		}
	}
	return false
}

// ProxyAPIKeyFromEnv reads PROXY_API_KEY from an exec-style KEY=value env slice.
func ProxyAPIKeyFromEnv(env []string) string {
	prefix := EnvAPIKey + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}
