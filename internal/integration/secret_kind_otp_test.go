package integration_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/backend/file"
)

// RFC 6238 seed "12345678901234567890" in Base32, the value Google's Key URI
// Format uses in its example. Synthetic only.
const rfcSeedB32 = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestSecretKindMetadata(t *testing.T) {
	env := storeEnv(t)
	if c, _, stderr := runKCSplit(env, "synthetic-only", "set", "site_login",
		"--no-verify", "--kind", "login", "--url", "https://example.test/", "--username", "octocat"); c != 0 {
		t.Fatalf("set with kind failed: %s", stderr)
	}
	if c, _, _ := runKCSplit(env, "synthetic-only", "set", "plain_token", "--no-verify"); c != 0 {
		t.Fatal("plain set failed")
	}

	c, meta, _ := runKCSplit(env, "", "ls", "--json")
	if c != 0 {
		t.Fatal("ls --json failed")
	}
	for _, want := range []string{`"kind": "login"`, `"url": "https://example.test/"`, `"username": "octocat"`} {
		if !strings.Contains(meta, want) {
			t.Fatalf("missing %s in %s", want, meta)
		}
	}
	idx := strings.Index(meta, `"name": "plain_token"`)
	if idx < 0 {
		t.Fatal("plain entry missing")
	}
	rest := meta[idx:]
	if end := strings.Index(rest, "}"); end > 0 {
		rest = rest[:end]
	}
	if strings.Contains(rest, `"kind"`) {
		t.Fatalf("unclassified entry must omit kind: %s", rest)
	}

	if c, _, _ := runKCSplit(env, "synthetic-only", "set", "bad_kind", "--no-verify", "--kind", "creditcard"); c == 0 {
		t.Fatal("unknown kind accepted")
	}
	if c, _, _ := runKCSplit(env, "synthetic-only", "set", "bad_url", "--no-verify", "--url", "https://example.test/"); c == 0 {
		t.Fatal("--url without --kind login accepted")
	}
}

func TestAppleCSVRoundTripsKindURLAndUsername(t *testing.T) {
	source := storeEnv(t)
	if c, _, stderr := runKCSplit(source, "synthetic-only", "set", "site_login", "--no-verify",
		"--kind", "login", "--url", "https://example.test/login", "--username", "octocat"); c != 0 {
		t.Fatalf("set failed: %s", stderr)
	}
	out := filepath.Join(t.TempDir(), "passwords.csv")
	if c, _, stderr := runKCSplit(source, "", "export", "site_login", "--format", "csv",
		"--allow-plaintext", "--output-file", out); c != 0 {
		t.Fatalf("export failed: %s", stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "site_login,https://example.test/login,octocat,synthetic-only,,") {
		t.Fatalf("csv row lost url/username: %q", string(data))
	}

	target := storeEnv(t)
	if c, _, stderr := runKCSplit(target, "", "import", "--format", "csv",
		"--allow-plaintext", "--input-file", out); c != 0 {
		t.Fatalf("import failed: %s", stderr)
	}
	c, meta, _ := runKCSplit(target, "", "ls", "--json")
	if c != 0 || !strings.Contains(meta, `"kind": "login"`) ||
		!strings.Contains(meta, `"url": "https://example.test/login"`) ||
		!strings.Contains(meta, `"username": "octocat"`) {
		t.Fatalf("round trip lost metadata: %s", meta)
	}
}

func TestOTPCommandAndExecInjection(t *testing.T) {
	env := storeEnv(t)
	uri := "otpauth://totp?secret=" + rfcSeedB32 + "&algorithm=SHA1&digits=8&period=30"
	if c, _, stderr := runKCSplit(env, uri, "set", "demo_otp", "--no-verify", "--kind", "otp"); c != 0 {
		t.Fatalf("set otp failed: %s", stderr)
	}

	// A pipe must not receive a code without --stdout.
	c, _, stderr := runKCSplit(env, "", "otp", "demo_otp", "--at", "1970-01-01T00:00:59Z")
	if c == 0 || !strings.Contains(stderr, "--stdout") {
		t.Fatalf("piped otp printed a code: %d %q", c, stderr)
	}

	c, code, stderr := runKCSplit(env, "", "otp", "demo_otp", "--at", "1970-01-01T00:00:59Z", "--stdout")
	if c != 0 || strings.TrimSpace(code) != "94287082" {
		t.Fatalf("otp = %q (%d) %s", code, c, stderr)
	}

	// The seed is not injected into children by default.
	c, out, _ := runKCSplit(env, "", "exec", "--", "sh", "-c", `env | grep -c "^DEMO_OTP=" || true`)
	if c != 0 || strings.TrimSpace(out) != "0" {
		t.Fatalf("seed leaked into child env: %q", out)
	}

	c, out, stderr = runKCSplit(env, "", "exec", "--otp", "demo_otp", "--", "sh", "-c", `printf %s "$DEMO_OTP"`)
	if c != 0 {
		t.Fatalf("exec --otp failed: %s", stderr)
	}
	if strings.TrimSpace(out) != "94287082" && !regexp.MustCompile(`^\d{8}$`).MatchString(strings.TrimSpace(out)) {
		t.Fatalf("exec --otp did not inject a code: %q", out)
	}
}

func TestCopyOTPToClipboard(t *testing.T) {
	// The clipboard tool is a test hook, so the store config must allow hooks;
	// without it the child kc would reach for the real pbcopy.
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "kc")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
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
		"KC_CLIP_PBCOPY=" + pbcopy,
		"KC_CLIP_PBPASTE=" + pbpaste,
		"KC_FAKE_CLIPBOARD=" + clipFile,
		"KC_CP_CLEAR_SECONDS=1",
	}
	uri := "otpauth://totp?secret=" + rfcSeedB32 + "&digits=6&period=30"
	if c, _, stderr := runKCSplit(env, uri, "set", "demo_otp", "--no-verify", "--kind", "otp"); c != 0 {
		t.Fatalf("set failed: %s", stderr)
	}
	if c, _, stderr := runKCSplit(env, "", "cp", "demo_otp", "--otp"); c != 0 {
		t.Fatalf("cp --otp failed: %s", stderr)
	}
	copied, err := os.ReadFile(clipFile)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\d{6}$`).MatchString(strings.TrimSpace(string(copied))) {
		t.Fatalf("clipboard did not hold a six-digit code: %q", copied)
	}
}

func TestCSVExportMergesSeedIntoItsLoginRow(t *testing.T) {
	env := storeEnv(t)
	if c, _, stderr := runKCSplit(env, "synthetic-only", "set", "site_login", "--no-verify",
		"--kind", "login", "--url", "https://example.test/", "--username", "octocat"); c != 0 {
		t.Fatalf("set login failed: %s", stderr)
	}
	uri := "otpauth://totp?secret=" + rfcSeedB32 + "&digits=6&period=30"
	if c, _, stderr := runKCSplit(env, uri, "set", "site_login.otp", "--no-verify", "--kind", "otp"); c != 0 {
		t.Fatalf("set otp failed: %s", stderr)
	}

	// Only the login name is exported; the seed must still reach OTPAuth.
	out := filepath.Join(t.TempDir(), "passwords.csv")
	c, _, stderr := runKCSplit(env, "", "export", "site_login", "--format", "csv",
		"--allow-plaintext", "--output-file", out)
	if c != 0 {
		t.Fatalf("export failed: %s", stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), uri) {
		t.Fatalf("seed not merged into the login row: %q", string(data))
	}
	if strings.Contains(string(data), "\r\nsite_login.otp,") {
		t.Fatalf("seed written as its own row: %q", string(data))
	}

	// Re-import keeps both entries (OTPAuth is opt-in on import).
	target := storeEnv(t)
	if c, _, stderr := runKCSplit(target, "", "import", "--format", "csv",
		"--allow-plaintext", "--keep-otp", "--input-file", out); c != 0 {
		t.Fatalf("import failed: %s", stderr)
	}
	c, meta, _ := runKCSplit(target, "", "ls", "--json")
	if c != 0 || !strings.Contains(meta, `"kind": "login"`) || !strings.Contains(meta, `"kind": "otp"`) {
		t.Fatalf("round trip lost a kind: %s", meta)
	}
}
