package integration_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestGuardedAgeImport(t *testing.T) {
	sender, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	other, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	senderEnv := []string{"HOME=" + t.TempDir(), "KC_KEY=" + sender.String()}
	receiverEnv := []string{"HOME=" + t.TempDir(), "KC_KEY=" + receiver.String()}
	if c, _, _ := runKCSplit(senderEnv, "synthetic-only", "set", "demo", "--no-verify"); c != 0 {
		t.Fatal("synthetic set failed")
	}
	bundlePath := filepath.Join(t.TempDir(), "synthetic.age")
	c, _, stderr := runKCSplit(senderEnv, "", "export", "missing", "--to", receiver.Recipient().String(), "--verified-recipient", other.Recipient().String(), "--output-file", bundlePath)
	if c == 0 || !strings.Contains(stderr, "recipient mismatch") {
		t.Fatal("mismatch must fail before secret lookup")
	}
	args := []string{"export", "demo", "--to", receiver.Recipient().String(), "--verified-recipient", receiver.Recipient().String(), "--output-file", bundlePath}
	c, stdout, _ := runKCSplit(senderEnv, "", args...)
	if c != 0 || stdout != "" {
		t.Fatal("synthetic export failed")
	}
	info, err := os.Stat(bundlePath)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("encrypted file permissions")
	}
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal("encrypted file missing")
	}
	bundle := string(data)
	c, _, _ = runKCSplit(senderEnv, "", args...)
	if c == 0 {
		t.Fatal("existing output overwritten")
	}
	c, _, _ = runKCSplit(senderEnv, "", "export", "demo", "--to", receiver.Recipient().String(), "--output-file", filepath.Join(t.TempDir(), "missing-check.age"))
	if c == 0 {
		t.Fatal("output without verified recipient accepted")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(bundle)))
	flags := []string{"import", "--expected-sha256", digest, "--verified-recipient", receiver.Recipient().String(), "--expected-name", "demo"}
	c, _, _ = runKCSplit(receiverEnv, bundle, "import", "--expected-sha256", digest)
	if c == 0 {
		t.Fatal("partial guard allowed")
	}
	c, _, _ = runKCSplit(receiverEnv, bundle, "import", "--expected-sha256", digest, "--verified-recipient", other.Recipient().String(), "--expected-name", "demo")
	if c == 0 {
		t.Fatal("wrong receiver allowed")
	}
	c, _, _ = runKCSplit(receiverEnv, bundle, "import", "--expected-sha256", strings.Repeat("0", 64), "--verified-recipient", receiver.Recipient().String(), "--expected-name", "demo")
	if c == 0 {
		t.Fatal("bad digest allowed")
	}
	c, _, _ = runKCSplit(receiverEnv, bundle, "import", "--expected-sha256", digest, "--verified-recipient", receiver.Recipient().String(), "--expected-name", "other")
	if c == 0 {
		t.Fatal("wrong name allowed")
	}
	c, _, _ = runKCSplit(receiverEnv, bundle, flags...)
	if c != 0 {
		t.Fatal("guarded import failed")
	}
	c, _, _ = runKCSplit(receiverEnv, bundle, flags...)
	if c == 0 {
		t.Fatal("overwrite allowed")
	}
	c, out, _ := runKCSplit(receiverEnv, "", "get", "demo")
	if c != 0 || strings.TrimSuffix(out, "\n") != "synthetic-only" {
		t.Fatal("synthetic value changed")
	}
}
