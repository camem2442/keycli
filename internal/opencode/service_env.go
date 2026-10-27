package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SyncBackgroundServiceEnv writes apiKey into the OpenCode background service env
// (service.json) and restarts the service so non-standalone `opencode run` can
// resolve {env:PROXY_API_KEY}. The secret is never passed on argv.
func SyncBackgroundServiceEnv(opencodeBin string, apiKey string) error {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("opencode service sync: empty %s", EnvAPIKey)
	}
	bin := strings.TrimSpace(opencodeBin)
	if bin == "" {
		bin = "opencode"
	}
	configDir, err := opencodeConfigDir(bin)
	if err != nil {
		return err
	}
	if err := runServiceCommand(bin, []string{"service", "stop"}, apiKey); err != nil {
		return err
	}
	if err := mergeServiceEnvFile(filepath.Join(configDir, "service.json"), EnvAPIKey, apiKey); err != nil {
		return err
	}
	return runServiceCommand(bin, []string{"service", "restart"}, apiKey)
}

func opencodeConfigDir(bin string) (string, error) {
	cmd := exec.Command(bin, "debug", "paths", "config")
	cmd.Env = serviceCommandEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("opencode debug paths config: %w (%s)", err, sanitizeServiceOutput(out, nil, ""))
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("opencode debug paths config: empty path")
	}
	return dir, nil
}

func runServiceCommand(bin string, args []string, redact string) error {
	cmd := exec.Command(bin, args...)
	cmd.Env = serviceCommandEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("opencode %s: %w (%s)", strings.Join(args, " "), err, sanitizeServiceOutput(out, err, redact))
	}
	return nil
}

func mergeServiceEnvFile(path string, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read service config: %w", err)
	}
	root := map[string]json.RawMessage{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse service config: %w", err)
		}
	}
	env := map[string]string{}
	if raw, ok := root["env"]; ok && len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("parse service env: %w", err)
		}
	}
	env[key] = value
	envRaw, err := json.Marshal(env)
	if err != nil {
		return err
	}
	root["env"] = envRaw
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp := filepath.Join(dir, fmt.Sprintf(".kc-service-%d.tmp", os.Getpid()))
	if err := os.WriteFile(temp, encoded, 0o600); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
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

func sanitizeServiceOutput(out []byte, err error, secret string) string {
	s := strings.TrimSpace(string(out))
	if err != nil {
		msg := err.Error()
		if s != "" {
			s = msg + ": " + s
		} else {
			s = msg
		}
	}
	if secret != "" {
		s = strings.ReplaceAll(s, secret, "[redacted]")
	}
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
	rest := argv[1:]
	if hasStandaloneFlag(rest) {
		return false
	}
	return firstOpenCodeSubcommand(rest) == "run"
}

func firstOpenCodeSubcommand(args []string) string {
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--" {
			return ""
		}
		if strings.HasPrefix(a, "-") {
			if strings.Contains(a, "=") {
				continue
			}
			switch a {
			case "-m", "--model", "-s", "--session", "--server", "-f", "--file":
				skipNext = true
			}
			continue
		}
		return a
	}
	return ""
}

func hasStandaloneFlag(args []string) bool {
	for _, a := range args {
		if a == "--standalone" || strings.HasPrefix(a, "--standalone=") {
			return true
		}
	}
	return false
}

// EffectiveProxyAPIKey returns the value a child process would see for PROXY_API_KEY
// (last duplicate wins, matching typical Unix env semantics).
func EffectiveProxyAPIKey(env []string) string {
	prefix := EnvAPIKey + "="
	var last string
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			last = e[len(prefix):]
		}
	}
	return last
}

// ProxyAPIKeyFromEnv reads PROXY_API_KEY from an exec-style KEY=value env slice.
func ProxyAPIKeyFromEnv(env []string) string {
	return EffectiveProxyAPIKey(env)
}
