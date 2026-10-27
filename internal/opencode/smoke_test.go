package opencode

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// KC_OPENCODE_NONSTANDALONE_SMOKE=1 on macOS with vertex-byok-proxy in kc store,
// opencode on PATH, and local proxy up. Never prints secret values.
func TestNonStandaloneOpenCodeSmoke(t *testing.T) {
	if os.Getenv("KC_OPENCODE_NONSTANDALONE_SMOKE") != "1" {
		t.Skip("set KC_OPENCODE_NONSTANDALONE_SMOKE=1 for real Mac smoke")
	}
	if _, err := exec.LookPath("opencode"); err != nil {
		t.Skip("opencode not on PATH")
	}
	kc := os.Getenv("KC_BIN")
	if kc == "" {
		var err error
		kc, err = exec.LookPath("kc")
		if err != nil {
			t.Skip("kc not on PATH")
		}
	}
	cmd := exec.Command(kc, "exec", "--only", "vertex-byok-proxy,PROXY_API_KEY", "--",
		"opencode", "run", "-m", "vertex-byok/google/gemini-3.7-flash", "Reply with exactly: OK")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("opencode run: %v\n%s", err, redactProxyKey(string(out)))
	}
	body := string(out)
	if !strings.Contains(body, "OK") {
		t.Fatalf("unexpected output (redacted): %s", redactProxyKey(body))
	}
}

func redactProxyKey(s string) string {
	// Best-effort: do not echo bearer material if opencode logs env errors.
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToUpper(line), "PROXY_API_KEY") && strings.Contains(line, "=") {
			lines[i] = "[redacted PROXY_API_KEY line]"
		}
	}
	return strings.Join(lines, "\n")
}
