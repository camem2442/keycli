package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/backend/file"
)

var kcBin, fakeSec, fakeClip string

func TestMain(m *testing.M) {
	root, err := moduleRoot()
	if err != nil {
		panic(err)
	}
	dir, err := os.MkdirTemp("", "kc-int-build")
	if err != nil {
		panic(err)
	}
	kcBin = filepath.Join(dir, "kc")
	fakeSec = filepath.Join(dir, "fakesecurity")
	fakeClip = filepath.Join(dir, "fakeclip")
	for _, spec := range []struct {
		out string
		pkg string
	}{
		{kcBin, filepath.Join(root, "cmd/kc")},
		{fakeSec, filepath.Join(root, "cmd/fakesecurity")},
		{fakeClip, filepath.Join(root, "cmd/fakeclip")},
	} {
		out, err := exec.Command("go", "build", "-o", spec.out, spec.pkg).CombinedOutput()
		if err != nil {
			panic(string(out))
		}
	}
	os.Exit(m.Run())
}

func moduleRoot() (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func TestKCEndToEnd(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, "sec.json")
	argvLog := filepath.Join(home, "argv.log")
	clipFile := filepath.Join(home, "clip.txt")
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	pbcopy := filepath.Join(binDir, "pbcopy")
	pbpaste := filepath.Join(binDir, "pbpaste")
	_ = os.Symlink(fakeClip, pbcopy)
	_ = os.Symlink(fakeClip, pbpaste)

	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	configToml := `
order = ["env", "store", "work"]

[backend.env]
type = "env"

[backend.store]
type = "file"
path = "` + storePath + `"

[backend.work]
type = "keychain"
service_prefix = "local.study.ai"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatal(err)
	}

	baseEnv := []string{
		"HOME=" + home,
		"KC_KEY=" + key,
		"KC_SECURITY=" + fakeSec,
		"KC_FAKE_SECURITY_STATE=" + state,
		"KC_FAKE_SECURITY_ARGV_LOG=" + argvLog,
		"KC_TEST_KEYCHAIN=1",
		"KC_CLIP_PBCOPY=" + pbcopy,
		"KC_CLIP_PBPASTE=" + pbpaste,
		"KC_FAKE_CLIPBOARD=" + clipFile,
		"KC_CP_CLEAR_SECONDS=1",
		"KC_CP_WAIT_CLEAR=1",
		"PATH=" + binDir,
	}

	code, out := runKC(baseEnv, "set", "demo")
	if code != 0 {
		t.Fatalf("set: %d %s", code, out)
	}
	code, out = runKC(baseEnv, "get", "demo")
	if code != 0 || strings.TrimSpace(out) != "piped-value" {
		t.Fatalf("get: %d %q", code, out)
	}

	code, out = runKC(append(baseEnv, "GH_TOKEN=from-env"), "ls", "--json")
	if code != 0 {
		t.Fatalf("ls: %d", code)
	}
	var payload struct {
		V     int `json:"v"`
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil || payload.V != 1 {
		t.Fatalf("json: %v", err)
	}

	code, _ = runKC(baseEnv, "get", "missing")
	if code != 2 {
		t.Fatalf("missing code %d", code)
	}

	envNoKey := withoutEnv(baseEnv, "KC_KEY")
	code, _ = runKC(envNoKey, "get", "demo")
	if code != 4 {
		t.Fatalf("locked code %d want 4", code)
	}

	code, _ = runKC(baseEnv, "typo")
	if code != 64 && code != 1 {
		t.Log("usage code", code)
	}

	// ai-core via fake keychain
	_ = os.WriteFile(state, []byte(`{"entries":{"local.study.ai.openai\topenai_api_key":"OAIPASS"}}`), 0o600)
	code, out = runKC(baseEnv, "get", "openai")
	if code != 0 || strings.TrimSpace(out) != "OAIPASS" {
		t.Fatalf("openai %d %q", code, out)
	}

	code, _ = runKC(baseEnv, "cp", "demo")
	if code != 0 {
		t.Fatal("cp failed")
	}
	exec.Command("sleep", "2").Run()
	if b, _ := os.ReadFile(clipFile); len(b) != 0 {
		t.Fatal("cp should auto-clear")
	}

	log, _ := os.ReadFile(argvLog)
	if strings.Contains(string(log), "piped-value") || strings.Contains(string(log), "OAIPASS") {
		t.Fatal("secret in argv log")
	}
}

func withoutEnv(env []string, key string) []string {
	out := make([]string, 0, len(env))
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func runKC(extraEnv []string, args ...string) (int, string) {
	if args[0] == "set" && len(args) == 2 {
		// pipe secret on stdin
		cmd := exec.Command(kcBin, args...)
		cmd.Env = append(os.Environ(), extraEnv...)
		cmd.Stdin = strings.NewReader("piped-value")
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err := cmd.Run()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		return code, buf.String()
	}
	cmd := exec.Command(kcBin, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return code, buf.String()
}
