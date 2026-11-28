package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

// TestAppleCSVExportImport covers the plaintext CSV path in both directions
// against a throwaway HOME, using synthetic values only.
func TestAppleCSVExportImport(t *testing.T) {
	env := storeEnv(t)
	for _, name := range []string{"demo_one", "demo_two"} {
		if c, _, _ := runKCSplit(env, "synthetic-only", "set", name, "--no-verify"); c != 0 {
			t.Fatalf("synthetic set %s failed", name)
		}
	}
	out := filepath.Join(t.TempDir(), "passwords.csv")

	// A plaintext file needs an explicit acknowledgement and a target path.
	if c, _, _ := runKCSplit(env, "", "export", "demo_one", "--format", "csv", "--output-file", out); c == 0 {
		t.Fatal("csv export without --allow-plaintext accepted")
	}
	if c, _, _ := runKCSplit(env, "", "export", "demo_one", "--format", "csv", "--allow-plaintext"); c == 0 {
		t.Fatal("csv export without --output-file accepted")
	}
	if c, _, _ := runKCSplit(env, "", "export", "demo_one", "--format", "csv", "--allow-plaintext",
		"--output-file", out, "--stdout"); c == 0 {
		t.Fatal("csv export combined with --stdout accepted")
	}
	if c, _, _ := runKCSplit(env, "", "export", "demo_one", "--format", "csv", "--allow-plaintext",
		"--output-file", out, "--to", ""); c == 0 {
		t.Fatal("csv export combined with an empty --to accepted")
	}

	c, stdout, stderr := runKCSplit(env, "", "export", "demo_one", "demo_two",
		"--format", "csv", "--allow-plaintext", "--output-file", out)
	if c != 0 {
		t.Fatalf("csv export failed: %s", stderr)
	}
	if stdout != "" {
		t.Fatal("csv export wrote to stdout")
	}
	if !strings.Contains(stderr, "2 row(s)") {
		t.Fatalf("summary missing: %q", stderr)
	}
	info, err := os.Stat(out)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("csv file mode: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "Title,URL,Username,Password,Notes,OTPAuth\r\n") {
		t.Fatalf("unexpected header: %q", string(data))
	}
	if strings.Count(string(data), "\r\n") != 3 {
		t.Fatalf("expected header plus two rows, got %q", string(data))
	}
	if c, _, _ := runKCSplit(env, "", "export", "demo_one", "--format", "csv",
		"--allow-plaintext", "--output-file", out); c == 0 {
		t.Fatal("existing csv file was overwritten")
	}

	// Import into a second store.
	receiver := storeEnv(t)
	if c, _, _ := runKCSplit(receiver, "", "import", "--format", "csv", "--input-file", out); c == 0 {
		t.Fatal("csv import without --allow-plaintext accepted")
	}
	if c, _, _ := runKCSplit(receiver, "", "import", "--format", "csv", "--allow-plaintext",
		"--input-file", out, "--expected-name", "demo_one"); c == 0 {
		t.Fatal("age guard accepted for a csv import")
	}
	c, _, stderr = runKCSplit(receiver, "", "import", "--format", "csv", "--allow-plaintext", "--input-file", out)
	if c != 0 {
		t.Fatalf("csv import failed: %s", stderr)
	}
	for _, name := range []string{"demo_one", "demo_two"} {
		c, value, _ := runKCSplit(receiver, "", "get", name)
		if c != 0 || strings.TrimRight(value, "\n") != "synthetic-only" {
			t.Fatalf("round-trip of %s failed", name)
		}
	}

	// Existing names need --force, like the age import path.
	if c, _, _ := runKCSplit(receiver, "", "import", "--format", "csv", "--allow-plaintext", "--input-file", out); c == 0 {
		t.Fatal("csv import overwrote existing names without --force")
	}
	if c, _, _ := runKCSplit(receiver, "", "import", "--format", "csv", "--allow-plaintext",
		"--input-file", out, "--force"); c != 0 {
		t.Fatal("csv import --force failed")
	}
}

func TestAppleCSVImportNamesAndVerificationCodes(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "export.csv")
	body := "Title,URL,Username,Password,Notes,OTPAuth\r\n" +
		"My Bank,https://example.test/,octocat,synthetic-one,note one,\r\n" +
		"my bank,,,synthetic-two,,\r\n" +
		"No password,,someone,,,\r\n" +
		"Site,,,synthetic-three,,otpauth://totp?secret=SYNTHETIC&algorithm=SHA1&digits=6&period=30\r\n"
	if err := os.WriteFile(csvPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	env := storeEnv(t)

	c, _, stderr := runKCSplit(env, "", "import", "--format", "csv", "--allow-plaintext",
		"--input-file", csvPath, "--dry-run")
	if c != 0 {
		t.Fatalf("dry run failed: %s", stderr)
	}
	if c, out, _ := runKCSplit(env, "", "ls"); c != 0 || strings.Contains(out, "my-bank") {
		t.Fatal("dry run wrote secrets")
	}

	c, _, stderr = runKCSplit(env, "", "import", "--format", "csv", "--allow-plaintext", "--input-file", csvPath)
	if c != 0 {
		t.Fatalf("import failed: %s", stderr)
	}
	if !strings.Contains(stderr, "without a password") || !strings.Contains(stderr, "verification code") {
		t.Fatalf("skip report missing: %q", stderr)
	}
	c, ls, _ := runKCSplit(env, "", "ls")
	if c != 0 {
		t.Fatal("ls failed")
	}
	for _, want := range []string{"my-bank", "my-bank-2", "site"} {
		if !strings.Contains(ls, want) {
			t.Fatalf("missing %s in %q", want, ls)
		}
	}
	if strings.Contains(ls, "site.otp") {
		t.Fatal("verification code stored without --keep-otp")
	}
	c, meta, _ := runKCSplit(env, "", "ls", "--json")
	if c != 0 || !strings.Contains(meta, "username=octocat") {
		t.Fatal("username not kept as metadata")
	}
	if strings.Contains(meta, "note one") {
		t.Fatal("Notes cell copied into agent-readable metadata without --keep-notes")
	}
	if c, _, _ := runKCSplit(env, "", "import", "--format", "csv", "--allow-plaintext",
		"--input-file", csvPath, "--dry-run"); c != 0 {
		t.Fatal("dry run refused a file with existing names")
	}

	kept := storeEnv(t)
	if c, _, stderr := runKCSplit(kept, "", "import", "--format", "csv", "--allow-plaintext",
		"--input-file", csvPath, "--keep-notes"); c != 0 {
		t.Fatalf("import --keep-notes failed: %s", stderr)
	}
	if c, keptMeta, _ := runKCSplit(kept, "", "ls", "--json"); c != 0 || !strings.Contains(keptMeta, "note one") {
		t.Fatal("--keep-notes did not keep the Notes cell")
	}

	second := storeEnv(t)
	c, _, stderr = runKCSplit(second, "", "import", "--format", "csv", "--allow-plaintext",
		"--input-file", csvPath, "--keep-otp")
	if c != 0 {
		t.Fatalf("import --keep-otp failed: %s", stderr)
	}
	c, value, _ := runKCSplit(second, "", "get", "site.otp")
	if c != 0 || !strings.HasPrefix(value, "otpauth://totp?") {
		t.Fatal("verification code not stored")
	}
}

// storeEnv returns a throwaway HOME with a synthetic age identity so the file
// store can take writes without touching the person's ~/.kc.
func storeEnv(t *testing.T) []string {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	return []string{"HOME=" + t.TempDir(), "KC_KEY=" + id.String()}
}
