package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
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
allow_test_hooks = true
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
	envGH := append(baseEnv, "GH_TOKEN=from-env")
	for _, it := range payload.Items {
		code, got := runKC(envGH, "get", it.Name)
		if code != 0 {
			t.Fatalf("ls listed %q but get failed: %d %s", it.Name, code, got)
		}
		if it.Name == "key" || it.Name == "agent" || strings.HasPrefix(it.Name, "test_") {
			t.Fatalf("internal KC_* variable leaked into ls: %q", it.Name)
		}
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
	if code != 64 {
		t.Fatalf("usage code %d want 64", code)
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

	_ = os.WriteFile(state, []byte(`{"entries":{"other-svc\ta1":"LINKED"}}`), 0o600)
	linkCfg := strings.Replace(configToml, `order = ["env", "store", "work"]`, `order = ["link", "env", "store", "work"]`, 1) + `
[backend.link]
type = "link"

[link.ext]
type = "keychain"
service = "other-svc"
account = "a1"
`
	_ = os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(linkCfg), 0o644)
	code, out = runKC(baseEnv, "link", "ext", "keychain:other-svc/a1")
	if code != 0 {
		t.Fatalf("link: %d %s", code, out)
	}
	code, out = runKC(baseEnv, "get", "ext")
	if code != 0 || strings.TrimSpace(out) != "LINKED" {
		t.Fatalf("linked get: %d %q", code, out)
	}
	code, out = runKC(baseEnv, "get", "ext", "--source")
	if code != 0 || !strings.Contains(out, "link:keychain:other-svc/a1") {
		t.Fatalf("source: %d %q", code, out)
	}
	code, out = runKC(baseEnv, "unlink", "ext")
	if code != 0 {
		t.Fatalf("unlink: %d", code)
	}

	code, out = runKC(baseEnv, "doctor", "--json")
	if code != 0 || !strings.Contains(out, `"config_path"`) {
		t.Fatalf("doctor: %d", code)
	}

	code, out = runKC(baseEnv, "rules", "--target", "cursor")
	if code != 0 || !strings.Contains(out, "kc ls --json") {
		t.Fatalf("rules: %d", code)
	}

	code, out = runKC(baseEnv, "setup-git", "--dry-run")
	if code != 0 || !strings.Contains(out, "git-credential") {
		t.Fatalf("setup-git: %d %q", code, out)
	}

	code, out = runKC(baseEnv, "setup-git", "--dry-run", "--host", "origin.test")
	if code != 0 || !strings.Contains(out, "credential.https://origin.test.helper") {
		t.Fatalf("setup-git host: %d %q", code, out)
	}
}

// TestInitIdentityStable covers P0-1: consecutive init runs must print the same
// recipient, --print-identity must expose that identity, and the identity must
// round-trip secrets through export/import.
func TestInitIdentityStable(t *testing.T) {
	home := t.TempDir()
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	configToml := `
allow_test_hooks = true
order = ["env", "store"]

[backend.env]
type = "env"

[backend.store]
type = "file"
path = "` + filepath.Join(home, ".kc", "store.age") + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatal(err)
	}
	// KC_KEY empty + a failing security runner keeps the real keychain out of
	// the test on macOS.
	env := []string{"HOME=" + home, "KC_KEY=", "KC_SECURITY=" + falseBin}

	code, out, stderr := runKCSplit(env, "", "init")
	if code != 0 {
		t.Fatalf("init: %d %s", code, stderr)
	}
	rec1 := strings.TrimSpace(out)
	if !strings.HasPrefix(rec1, "age1") {
		t.Fatalf("init recipient %q", rec1)
	}

	code, out, stderr = runKCSplit(env, "", "init")
	if code != 0 {
		t.Fatalf("second init: %d %s", code, stderr)
	}
	if rec2 := strings.TrimSpace(out); rec2 != rec1 {
		t.Fatalf("init recipient changed: %q != %q", rec2, rec1)
	}

	path := filepath.Join(home, ".kc", "receiver.key")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stored := strings.TrimSpace(string(raw))
	id, err := age.ParseX25519Identity(stored)
	if err != nil {
		t.Fatalf("receiver.key is not an identity: %v", err)
	}
	if id.Recipient().String() != rec1 {
		t.Fatalf("printed recipient %q does not match receiver.key %q", rec1, id.Recipient().String())
	}

	code, out, stderr = runKCSplit(env, "", "init", "--print-identity")
	if code != 0 {
		t.Fatalf("print-identity: %d %s", code, stderr)
	}
	identity := strings.TrimSpace(out)
	if identity != stored {
		t.Fatal("--print-identity did not return receiver.key")
	}

	// A public recipient is not an identity; fail with the locked exit code
	// instead of minting a new key.
	code, _, _ = runKCSplit([]string{"HOME=" + home, "KC_KEY=" + rec1, "KC_SECURITY=" + falseBin}, "", "init")
	if code != 4 {
		t.Fatalf("recipient as KC_KEY: exit %d, want 4", code)
	}

	envKey := append(env, "KC_KEY="+identity)
	code, _, stderr = runKCSplit(envKey, "roundtrip-value", "set", "demo")
	if code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	code, out, stderr = runKCSplit(envKey, "", "get", "demo")
	if code != 0 || strings.TrimSpace(out) != "roundtrip-value" {
		t.Fatalf("get: %d %q %s", code, out, stderr)
	}

	code, bundle, stderr := runKCSplit(envKey, "", "export", "demo", "--to", rec1)
	if code != 0 || len(bundle) == 0 {
		t.Fatalf("export: %d %s", code, stderr)
	}
	code, _, stderr = runKCSplit(envKey, "", "rm", "demo")
	if code != 0 {
		t.Fatalf("rm: %d %s", code, stderr)
	}
	code, _, stderr = runKCSplit(envKey, string(bundle), "import")
	if code != 0 {
		t.Fatalf("import: %d %s", code, stderr)
	}
	code, out, stderr = runKCSplit(envKey, "", "get", "demo")
	if code != 0 || strings.TrimSpace(out) != "roundtrip-value" {
		t.Fatalf("round-trip get: %d %q %s", code, out, stderr)
	}
}

// TestLockedStorePropagates covers P0-3: ls must fail with the locked exit code
// and exec must not start the child when the store cannot be decrypted.
func TestLockedStorePropagates(t *testing.T) {
	home := t.TempDir()
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	configToml := `
allow_test_hooks = true
order = ["env", "store"]

[backend.env]
type = "env"

[backend.store]
type = "file"
path = "` + filepath.Join(home, ".kc", "store.age") + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	withKey := []string{"HOME=" + home, "KC_KEY=" + key, "KC_SECURITY=" + falseBin}
	code, _, stderr := runKCSplit(withKey, "locked-value", "set", "demo")
	if code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}

	locked := []string{"HOME=" + home, "KC_KEY=", "KC_SECURITY=" + falseBin}
	code, out, stderr := runKCSplit(locked, "", "ls", "--json")
	if code != 4 {
		t.Fatalf("locked ls: exit %d want 4 (%s)", code, stderr)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("locked ls printed items: %q", out)
	}

	marker := filepath.Join(home, "exec-ran")
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ = runKCSplit(locked, "", "exec", "--", sh, "-c", `printf x > "$1"`, "sh", marker)
	if code != 4 {
		t.Fatalf("locked exec: exit %d want 4", code)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("exec ran the child without secrets")
	}

	code, _, _ = runKCSplit(locked, "", "get", "demo")
	if code != 4 {
		t.Fatalf("locked get: exit %d want 4", code)
	}
}

// TestExportDotenvAutoClear covers P0-4: `export --format dotenv` must schedule
// clipboard clearing in a detached child that outlives the export process.
func TestExportDotenvAutoClear(t *testing.T) {
	home := t.TempDir()
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	configToml := `
allow_test_hooks = true
order = ["env", "store"]

[backend.env]
type = "env"

[backend.store]
type = "file"
path = "` + filepath.Join(home, ".kc", "store.age") + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	clipFile := filepath.Join(home, "clip.txt")
	_ = os.WriteFile(clipFile, nil, 0o600)
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	pbcopy := filepath.Join(binDir, "pbcopy")
	pbpaste := filepath.Join(binDir, "pbpaste")
	if err := os.Symlink(fakeClip, pbcopy); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fakeClip, pbpaste); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"HOME=" + home,
		"KC_KEY=" + key,
		"KC_SECURITY=" + falseBin,
		"KC_CLIP_PBCOPY=" + pbcopy,
		"KC_CLIP_PBPASTE=" + pbpaste,
		"KC_FAKE_CLIPBOARD=" + clipFile,
		"KC_CP_CLEAR_SECONDS=1",
	}
	code, _, stderr := runKCSplit(env, "dotenv-value", "set", "demo")
	if code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}

	code, _, stderr = runKCSplit(env, "", "export", "demo", "--format", "dotenv")
	if code != 0 {
		t.Fatalf("export: %d %s", code, stderr)
	}
	copied, err := os.ReadFile(clipFile)
	if err != nil || len(copied) == 0 {
		t.Fatalf("export did not copy dotenv: %v %q", err, copied)
	}

	// A user copy during the delay wins: the child must not wipe it.
	_ = os.WriteFile(clipFile, []byte("user-changed"), 0o600)
	time.Sleep(1500 * time.Millisecond)
	if got, _ := os.ReadFile(clipFile); string(got) != "user-changed" {
		t.Fatalf("changed clipboard was cleared: %q", got)
	}

	code, _, stderr = runKCSplit(env, "", "export", "demo", "--format", "dotenv")
	if code != 0 {
		t.Fatalf("second export: %d %s", code, stderr)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		time.Sleep(100 * time.Millisecond)
		got, _ := os.ReadFile(clipFile)
		if len(got) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("export clipboard was not cleared: %q", got)
		}
	}
}

// TestBrokenPluginIsolated covers P0-5: a missing external backend must not
// break ls, doctor, plugin list, or other backends.
func TestBrokenPluginIsolated(t *testing.T) {
	home := t.TempDir()
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	configToml := `
allow_test_hooks = true
order = ["env", "store", "broken"]

[backend.env]
type = "env"

[backend.store]
type = "file"
path = "` + filepath.Join(home, ".kc", "store.age") + `"

[backend.broken]
type = "external"
plugin = "kc-backend-definitely-missing"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatal(err)
	}
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + key, "KC_SECURITY=" + falseBin}
	code, _, stderr := runKCSplit(env, "isolated-value", "set", "demo")
	if code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}

	code, out, stderr := runKCSplit(env, "", "ls", "--json")
	if code != 0 {
		t.Fatalf("ls with broken plugin: %d %s", code, stderr)
	}
	if !strings.Contains(out, `"demo"`) {
		t.Fatalf("ls missing store item: %s", out)
	}

	code, out, stderr = runKCSplit(env, "", "get", "demo")
	if code != 0 || strings.TrimSpace(out) != "isolated-value" {
		t.Fatalf("get with broken plugin: %d %q %s", code, out, stderr)
	}

	code, out, stderr = runKCSplit(env, "", "doctor", "--json")
	if code != 0 || !strings.Contains(out, `"config_path"`) {
		t.Fatalf("doctor with broken plugin: %d %s", code, stderr)
	}
	if !strings.Contains(out, "definitely-missing") && !strings.Contains(out, "broken") {
		t.Fatalf("doctor should report the broken backend: %s", out)
	}

	code, out, stderr = runKCSplit(env, "", "plugin", "list", "--json")
	if code != 0 || !strings.Contains(out, "definitely-missing") {
		t.Fatalf("plugin list with broken plugin: %d %s %s", code, out, stderr)
	}
}

// TestExitCodeContract covers P0-7: cobra usage errors are 64 and a first run
// without any identity is 4.
func TestExitCodeContract(t *testing.T) {
	home := t.TempDir()
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	configToml := `
allow_test_hooks = true
order = ["env", "store"]

[backend.env]
type = "env"

[backend.store]
type = "file"
path = "` + filepath.Join(home, ".kc", "store.age") + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=", "KC_SECURITY=" + falseBin}
	cases := []struct {
		args []string
		want int
	}{
		{[]string{"typo"}, 64},
		{[]string{"ls", "--bogus"}, 64},
		{[]string{"get"}, 64},
		{[]string{"get", "a", "b"}, 64},
		{[]string{"export"}, 64},
		{[]string{"cp", "a", "b"}, 64},
		{[]string{"exec"}, 64},
	}
	for _, tc := range cases {
		code, out, stderr := runKCSplit(env, "", tc.args...)
		if code != tc.want {
			t.Fatalf("kc %v: exit %d want %d (stdout=%q stderr=%q)", tc.args, code, tc.want, out, stderr)
		}
	}

	code, _, stderr := runKCSplit(env, "fresh-value", "set", "fresh")
	if code != 4 {
		t.Fatalf("fresh set without identity: exit %d want 4 (%s)", code, stderr)
	}
	if !strings.Contains(stderr, "KC_KEY") && !strings.Contains(stderr, "kc init") {
		t.Fatalf("fresh set should point at identity setup: %q", stderr)
	}
}

// TestGitCredentialHostMap covers the configurable git credential helper:
// a mapped host resolves its secret, optionally transforms it with a command,
// and unmapped hosts stay silent.
func TestGitCredentialHostMap(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	configToml := `
allow_test_hooks = true
order = ["store"]

[backend.store]
type = "file"
path = "` + storePath + `"

[git."origin.test"]
secret = "cursor_api_key"
username = "x-access-token"
command = "printf 'JWT-%s' \"$CURSOR_API_KEY\""

[git."plain.test"]
secret = "plain_token"

[git."strict.test"]
secret = "missing_token"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o644); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + key}

	code, out := runKC(env, "set", "cursor_api_key")
	if code != 0 {
		t.Fatalf("set: %d %s", code, out)
	}
	code, out = runKC(env, "set", "plain_token")
	if code != 0 {
		t.Fatalf("set plain: %d %s", code, out)
	}

	code, out = runKCInput(env, "protocol=https\nhost=origin.test\n\n", "git-credential", "get")
	if code != 0 {
		t.Fatalf("git-credential: %d %q", code, out)
	}
	if !strings.Contains(out, "username=x-access-token") || !strings.Contains(out, "password=JWT-piped-value") {
		t.Fatalf("command mapping: %q", out)
	}

	code, out = runKCInput(env, "protocol=https\nhost=plain.test\n\n", "git-credential", "get")
	if code != 0 || !strings.Contains(out, "username=git") || !strings.Contains(out, "password=piped-value") {
		t.Fatalf("plain mapping: %d %q", code, out)
	}

	code, out = runKCInput(env, "protocol=https\nhost=unmapped.test\n\n", "git-credential", "get")
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("unmapped host must be silent: %d %q", code, out)
	}

	// The built-in github.com default is implicit: with no github secret it
	// must stay silent instead of failing git's credential chain.
	code, out = runKCInput(env, "protocol=https\nhost=github.com\n\n", "git-credential", "get")
	if code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("implicit github.com default must be silent without a secret: %d %q", code, out)
	}

	// An explicit mapping is enforced: a missing secret is an error.
	code, out = runKCInput(env, "protocol=https\nhost=strict.test\n\n", "git-credential", "get")
	if code != 2 || !strings.Contains(out, "missing_token") {
		t.Fatalf("explicit mapping must report a missing secret: %d %q", code, out)
	}

	code, out = runKCInput(env, "protocol=https\nhost=origin.test\n\n", "git-credential", "store")
	if code != 0 {
		t.Fatalf("store must be no-op: %d %q", code, out)
	}
}

func TestSetupGitRecordsMapping(t *testing.T) {
	home := t.TempDir()
	gitCfg := filepath.Join(home, "gitconfig")
	env := []string{"HOME=" + home, "GIT_CONFIG_GLOBAL=" + gitCfg}
	cfgPath := filepath.Join(home, ".config", "kc", "config.toml")

	code, out := runKC(env, "setup-git", "--host", "origin.test")
	if code != 0 {
		t.Fatalf("setup-git: %d %q", code, out)
	}
	if !strings.Contains(out, "recorded") {
		t.Fatalf("setup-git must report the recorded mapping: %q", out)
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "origin.test") || !strings.Contains(body, `secret = "origin.test"`) {
		t.Fatalf("mapping not recorded in %s:\n%s", cfgPath, body)
	}

	code, out = runKC(env, "setup-git", "--host", "origin.test")
	if code != 0 || strings.Contains(out, "recorded") {
		t.Fatalf("second setup-git must not rewrite the mapping: %d %q", code, out)
	}

	code, out = runKC(env, "setup-git", "--dry-run", "--host", "dry.test")
	if code != 0 || !strings.Contains(out, "would record") {
		t.Fatalf("dry-run must preview the mapping: %d %q", code, out)
	}
	data, err = os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "dry.test") {
		t.Fatalf("dry-run wrote config:\n%s", data)
	}
}

// TestSetInputAndClipboardSafety covers P1-4: piped input loses exactly one
// trailing newline, and --clip keeps the clipboard when the save fails.
func TestSetInputAndClipboardSafety(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	falseBin, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	configToml := `
allow_test_hooks = true
order = ["store"]

[backend.store]
type = "file"
path = "` + storePath + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + key, "KC_SECURITY=" + falseBin}

	code, _, stderr := runKCSplit(env, "line-value\n", "set", "demo")
	if code != 0 {
		t.Fatalf("set: %d %s", code, stderr)
	}
	code, out, stderr := runKCSplit(env, "", "get", "-n", "demo")
	if code != 0 || out != "line-value" {
		t.Fatalf("stdin newline not trimmed exactly once: %d %q %s", code, out, stderr)
	}

	clipFile := filepath.Join(home, "clip.txt")
	_ = os.WriteFile(clipFile, []byte("user-copy"), 0o600)
	binDir := filepath.Join(home, "bin")
	_ = os.MkdirAll(binDir, 0o755)
	pbcopy := filepath.Join(binDir, "pbcopy")
	pbpaste := filepath.Join(binDir, "pbpaste")
	if err := os.Symlink(fakeClip, pbcopy); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fakeClip, pbpaste); err != nil {
		t.Fatal(err)
	}
	lockedEnv := []string{
		"HOME=" + home,
		"KC_SECURITY=" + falseBin,
		"KC_CLIP_PBCOPY=" + pbcopy,
		"KC_CLIP_PBPASTE=" + pbpaste,
		"KC_FAKE_CLIPBOARD=" + clipFile,
	}
	code, _, _ = runKCSplit(lockedEnv, "", "set", "demo", "--clip")
	if code == 0 {
		t.Fatal("set --clip must fail without an identity")
	}
	if got, _ := os.ReadFile(clipFile); string(got) != "user-copy" {
		t.Fatalf("failed save cleared the clipboard: %q", got)
	}
}

// TestExecStripsIdentityEnv covers P1-3: the child process must not inherit
// kc's identity (KC_KEY) or agent identity (KC_AGENT).
func TestExecStripsIdentityEnv(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	configToml := `
allow_test_hooks = true
order = ["store"]

[backend.store]
type = "file"
path = "` + storePath + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + key, "KC_AGENT=ci"}
	if code, out := runKC(env, "set", "demo"); code != 0 {
		t.Fatalf("set: %d %s", code, out)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	code, out := runKC(env, "exec", "--", sh, "-c", `printf '%s|%s|%s' "${KC_KEY:-unset}" "${KC_AGENT:-unset}" "${DEMO:-unset}"`)
	if code != 0 {
		t.Fatalf("exec: %d %q", code, out)
	}
	if strings.TrimSpace(out) != "unset|unset|piped-value" {
		t.Fatalf("child env leaked identity or lost secret: %q", out)
	}
}

// TestCLIContractDetails covers P1-9: ls --json keeps false fields, audit
// --json returns [] instead of null, and status reports unreadable names.
func TestCLIContractDetails(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	configToml := `
allow_test_hooks = true
order = ["store"]

[backend.store]
type = "file"
path = "` + storePath + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + key}
	if code, out := runKC(env, "set", "demo"); code != 0 {
		t.Fatalf("set: %d %s", code, out)
	}

	code, out := runKC(env, "ls", "--json")
	if code != 0 {
		t.Fatalf("ls: %d %s", code, out)
	}
	for _, field := range []string{`"stale_export": false`, `"policy_denied": false`, `"expires": null`} {
		if !strings.Contains(out, field) {
			t.Fatalf("ls --json missing %s:\n%s", field, out)
		}
	}

	code, out = runKC(env, "audit", "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("audit --json without log must be []: %d %q", code, out)
	}
	if err := os.MkdirAll(filepath.Join(home, ".kc"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".kc", "audit.log"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	code, out = runKC(env, "audit", "--json")
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("audit --json with empty log must be []: %d %q", code, out)
	}

	ledgerPath := filepath.Join(home, ".kc", "ledger.json")
	entries := `[
  {"name":"gone","target":"cursor","hmac":"deadbeef","exported_at":"2026-01-01T00:00:00Z"}
]`
	if err := os.WriteFile(ledgerPath, []byte(entries), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out = runKC(env, "status")
	if code != 0 || !strings.Contains(out, "확인 실패") || !strings.Contains(out, "gone") {
		t.Fatalf("status must report unreadable names: %d %q", code, out)
	}
	if strings.Contains(out, "기록된보내기") || strings.Contains(out, "마지막보내기") {
		t.Fatalf("status typo remains: %q", out)
	}
}

// TestDoctorReportsIdentitySource covers P1-8: doctor reports which of the
// three identity sources is in effect (env, receiver.key, keychain).
func TestDoctorReportsIdentitySource(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	configToml := `
allow_test_hooks = true
order = ["store"]

[backend.store]
type = "file"
path = "` + storePath + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, "sec.json")
	keychainKey, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	seed, _ := json.Marshal(map[string]any{
		"entries": map[string]string{"kc.age\tkc": keychainKey},
	})
	if err := os.WriteFile(state, seed, 0o600); err != nil {
		t.Fatal(err)
	}
	base := []string{
		"HOME=" + home,
		"KC_SECURITY=" + fakeSec,
		"KC_TEST_KEYCHAIN=1",
		"KC_FAKE_SECURITY_STATE=" + state,
	}

	read := func(env []string) (string, string) {
		code, out := runKC(env, "doctor", "--json")
		if code != 0 {
			t.Fatalf("doctor: %d %s", code, out)
		}
		var rep struct {
			KCAge        string `json:"kc_age"`
			IdentityPath string `json:"identity_path"`
		}
		if err := json.Unmarshal([]byte(out), &rep); err != nil {
			t.Fatal(err)
		}
		return rep.KCAge, rep.IdentityPath
	}

	if src, path := read(base); src != "keychain" || path != "" {
		t.Fatalf("keychain source: %q %q", src, path)
	}

	fileKey, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	receiver := filepath.Join(home, ".kc", "receiver.key")
	_ = os.MkdirAll(filepath.Dir(receiver), 0o700)
	if err := os.WriteFile(receiver, []byte(fileKey+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if src, path := read(base); src != "file" || path != receiver {
		t.Fatalf("file source: %q %q", src, path)
	}

	envKey, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	if src, path := read(append(base, "KC_KEY="+envKey)); src != "env" || path != "" {
		t.Fatalf("env source: %q %q", src, path)
	}
}

// TestConcurrentSetKeepsAllNames covers P1-6: concurrent processes must not
// lose each other's read-modify-write updates to the file store.
func TestConcurrentSetKeepsAllNames(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	configToml := `
allow_test_hooks = true
order = ["store"]

[backend.store]
type = "file"
path = "` + storePath + `"
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + key}

	const n = 8
	code := make([]int, n)
	stderr := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			code[i], _, stderr[i] = runKCSplit(env, "value-"+strconv.Itoa(i), "set", "conc."+strconv.Itoa(i))
		}(i)
	}
	wg.Wait()
	for i := 0; i < n; i++ {
		if code[i] != 0 {
			t.Fatalf("concurrent set %d: %d %s", i, code[i], stderr[i])
		}
	}

	for i := 0; i < n; i++ {
		name := "conc." + strconv.Itoa(i)
		got, out, errOut := runKCSplit(env, "", "get", "-n", name)
		if got != 0 || out != "value-"+strconv.Itoa(i) {
			t.Fatalf("%s lost or corrupted: %d %q %s", name, got, out, errOut)
		}
	}
}

// TestLSStaleAuditContext covers P1-2: stale annotation is opt-in, reads only
// names in the ledger, never reads policy-denied names, and records the
// invoking command in audit lines.
func TestLSStaleAuditContext(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(home, ".config", "kc")
	_ = os.MkdirAll(cfgDir, 0o755)
	storePath := filepath.Join(home, ".kc", "store.age")
	auditPath := filepath.Join(home, ".kc", "audit.log")
	configToml := `
allow_test_hooks = true
order = ["store"]
middleware = ["audit", "policy"]

[backend.store]
type = "file"
path = "` + storePath + `"

[audit]
path = "` + auditPath + `"

[policy.agent.ci]
allow = ["allowed"]
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(configToml), 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + key, "KC_AGENT=ci"}

	for _, name := range []string{"allowed", "denied"} {
		if code, out := runKC(env, "set", name); code != 0 {
			t.Fatalf("set %s: %d %s", name, code, out)
		}
	}
	_ = os.Remove(auditPath)

	// Without --stale, ls must not read values at all.
	if code, out := runKC(env, "ls"); code != 0 {
		t.Fatalf("ls: %d %s", code, out)
	}
	if data, err := os.ReadFile(auditPath); err == nil && len(data) > 0 {
		t.Fatalf("plain ls must not read values:\n%s", data)
	}

	ledgerPath := filepath.Join(home, ".kc", "ledger.json")
	entries := `[
  {"name":"allowed","target":"cursor","hmac":"deadbeef","exported_at":"2026-01-01T00:00:00Z"},
  {"name":"denied","target":"cursor","hmac":"deadbeef","exported_at":"2026-01-01T00:00:00Z"}
]`
	if err := os.WriteFile(ledgerPath, []byte(entries), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out := runKC(env, "ls", "--stale", "--all")
	if code != 0 {
		t.Fatalf("ls --stale: %d %s", code, out)
	}
	if !strings.Contains(out, "allowed") || !strings.Contains(out, "denied") {
		t.Fatalf("ls --all output: %q", out)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("want exactly one audited read, got %d:\n%s", len(lines), data)
	}
	var rec struct {
		Command string `json:"command"`
		Name    string `json:"name"`
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Command != "ls" || rec.Name != "allowed" || rec.Outcome != "ok" {
		t.Fatalf("audit record: %+v", rec)
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

// runKCInput runs kc with the given stdin payload (for git-credential).
func runKCInput(extraEnv []string, stdin string, args ...string) (int, string) {
	cmd := exec.Command(kcBin, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin = strings.NewReader(stdin)
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

// runKCSplit keeps stdout and stderr separate (for commands that print
// machine-readable values to stdout).
func runKCSplit(extraEnv []string, stdin string, args ...string) (int, string, string) {
	cmd := exec.Command(kcBin, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return code, stdout.String(), stderr.String()
}
