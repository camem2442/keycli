package integration_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/agebundle"
	"github.com/camem2442/keycli/internal/recipientattest"
)

func TestRecipientAttestedExport(t *testing.T) {
	home := t.TempDir()
	sender, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust := recipientattest.Trust{Version: 1, Issuer: "https://synthetic.invalid/issuer", PublicKey: base64.StdEncoding.EncodeToString(pub), Environment: "synthetic-environment"}
	trustPath := filepath.Join(t.TempDir(), "trust.json")
	data, _ := json.Marshal(trust)
	if err := os.WriteFile(trustPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	env := []string{"HOME=" + home, "KC_KEY=" + sender.String()}
	recipient := receiver.Recipient().String()
	requestPath := filepath.Join(t.TempDir(), "request.json")
	code, stdout, stderr := runKCSplit(env, "", "recipient-challenge", "--attestation-trust", trustPath, "--to", recipient, "--environment", trust.Environment, "--name", "demo", "--output-file", requestPath)
	if code != 0 || stdout != "" {
		t.Fatalf("challenge failed: %d %s", code, stderr)
	}
	data, err = os.ReadFile(requestPath)
	if err != nil {
		t.Fatal(err)
	}
	var request recipientattest.Request
	if err = json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(request)
	signed := func(key ed25519.PrivateKey) []byte {
		sig := ed25519.Sign(key, append([]byte(recipientattest.SigningDomain), payload...))
		out, _ := json.Marshal(recipientattest.Envelope{Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(sig)})
		return out
	}
	attestationPath := filepath.Join(t.TempDir(), "attestation.json")
	_, attacker, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(attestationPath, signed(attacker), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "bundle.age")
	flags := []string{"--to", recipient, "--output-file", output, "--recipient-attestation", attestationPath, "--attestation-trust", trustPath, "--attestation-challenge", request.Nonce}
	args := append([]string{"export", "demo"}, flags...)
	code, stdout, stderr = runKCSplit(env, "", args...)
	if code == 0 || stdout != "" || !strings.Contains(stderr, "untrusted recipient attestation signature") {
		t.Fatal("bad issuer must fail before absent secret lookup")
	}
	for _, partial := range [][]string{{"--recipient-attestation", attestationPath}, {"--attestation-trust", trustPath}, {"--attestation-challenge", request.Nonce}, {"--recipient-attestation="}} {
		a := append([]string{"export", "demo", "--to", recipient, "--output-file", output}, partial...)
		code, _, _ = runKCSplit(env, "", a...)
		if code == 0 {
			t.Fatal("partial opt-in accepted")
		}
	}
	if err = os.WriteFile(attestationPath, signed(private), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ = runKCSplit(env, "", append(args, "--verified-recipient", recipient)...)
	if code == 0 {
		t.Fatal("manual flag fallback accepted")
	}
	if code, _, _ = runKCSplit(env, "synthetic-only", "set", "demo", "--no-verify"); code != 0 {
		t.Fatal("synthetic set failed")
	}
	code, stdout, stderr = runKCSplit(env, "", args...)
	if code != 0 || stdout != "" {
		t.Fatalf("attested export failed: %d %s", code, stderr)
	}
	if st, e := os.Stat(output); e != nil || st.Mode().Perm() != 0o600 {
		t.Fatal("encrypted output missing or not private")
	}
	ciphertext, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := agebundle.Decrypt(bytes.NewReader(ciphertext), receiver)
	if err != nil || len(recovered) != 1 || string(recovered["demo"]) != "synthetic-only" {
		t.Fatal("attested export did not encrypt the single expected synthetic value for the bound recipient")
	}
	code, _, stderr = runKCSplit(env, "", args...)
	if code == 0 || !strings.Contains(stderr, "already consumed") {
		t.Fatal("replayed attestation accepted")
	}
}

func TestInvalidAttestationDoesNotStartExternalBackend(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "kc")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "plugin-started")
	plugin := filepath.Join(home, "kc-backend-probe")
	if err := os.WriteFile(plugin, []byte("#!/bin/sh\n: > '"+marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "order = [\"probe\"]\n[backend.probe]\ntype = \"external\"\ncommand = \"" + plugin + "\"\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	receiver, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := runKCSplit([]string{"HOME=" + home}, "", "export", "missing", "--to", receiver.Recipient().String(), "--output-file", filepath.Join(home, "out.age"), "--recipient-attestation", filepath.Join(home, "missing.json"))
	if code == 0 {
		t.Fatal("partial attestation accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("backend started before attestation gate")
	}
}

func TestAttestedExportPreservesLatestDefaultPolicy(t *testing.T) {
	home := t.TempDir()
	cfgDir := filepath.Join(home, ".config", "kc")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "middleware = [\"policy\"]\n[policy.agent.default]\nallow = []\n[alias]\ndemo = [\"fixture_alias\"]\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	receiver, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	trust := recipientattest.Trust{Version: 1, Issuer: "https://synthetic.invalid/issuer", PublicKey: base64.StdEncoding.EncodeToString(pub), Environment: "synthetic-environment"}
	trustPath := filepath.Join(t.TempDir(), "trust.json")
	data, _ := json.Marshal(trust)
	if err := os.WriteFile(trustPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	request, err := recipientattest.CreateChallenge(home, trust, receiver.Recipient().String(), trust.Environment, "demo", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(request)
	sig := ed25519.Sign(private, append([]byte(recipientattest.SigningDomain), payload...))
	envelope, _ := json.Marshal(recipientattest.Envelope{Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(sig)})
	envelopePath := filepath.Join(t.TempDir(), "envelope.json")
	if err := os.WriteFile(envelopePath, envelope, 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "bundle.age")
	code, stdout, _ := runKCSplit([]string{"HOME=" + home, "KC_AGENT=unregistered-fixture-agent", "DEMO=synthetic-only"}, "", "export", "fixture_alias", "--to", receiver.Recipient().String(), "--output-file", output, "--recipient-attestation", envelopePath, "--attestation-trust", trustPath, "--attestation-challenge", request.Nonce)
	if code != 5 || stdout != "" {
		t.Fatal("valid attestation bypassed the latest default-deny policy")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("policy-denied export produced output")
	}
	if _, err := os.Stat(filepath.Join(home, ".kc", "recipient-challenges", request.Nonce+".used")); err != nil {
		t.Fatal("attestation was not verified and consumed before policy denial")
	}
}
