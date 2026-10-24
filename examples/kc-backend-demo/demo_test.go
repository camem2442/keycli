package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDemoPluginHandshakeAndSetGet(t *testing.T) {
	home := t.TempDir()
	bin := buildDemo(t)
	env := []string{"HOME=" + home}

	resp := runPlugin(t, bin, env, map[string]any{"v": 1, "op": "handshake", "backend": "demo"})
	if !resp["ok"].(bool) {
		t.Fatalf("handshake: %v", resp)
	}

	runPlugin(t, bin, env, map[string]any{
		"v": 1, "op": "set", "backend": "demo", "name": "x",
		"value_base64": "dGVzdA==",
	})
	got := runPlugin(t, bin, env, map[string]any{"v": 1, "op": "get", "backend": "demo", "name": "x"})
	if got["value_base64"] != "dGVzdA==" {
		t.Fatalf("get: %v", got)
	}
}

func buildDemo(t *testing.T) string {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "kc-backend-demo")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = root
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, b)
	}
	return out
}

func runPlugin(t *testing.T, bin string, env []string, req map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(req)
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	line := strings.TrimSpace(string(out))
	var resp map[string]any
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("json: %v %q", err, line)
	}
	return resp
}
