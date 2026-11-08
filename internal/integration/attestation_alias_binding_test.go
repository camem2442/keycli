package integration_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/agebundle"
	"github.com/camem2442/keycli/internal/recipientattest"
)

type aliasAttestationFixture struct {
	home, trustPath, dir, marker string
	trust                        recipientattest.Trust
	issuer                       ed25519.PrivateKey
	receiver                     *age.X25519Identity
}

func newAliasAttestationFixture(t *testing.T, aliases string) aliasAttestationFixture {
	t.Helper()
	f := aliasAttestationFixture{home: t.TempDir(), dir: t.TempDir()}
	cfgDir := filepath.Join(f.home, ".config", "kc")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	f.marker = filepath.Join(f.home, "backend-started")
	plugin := filepath.Join(f.home, "kc-backend-probe")
	if err := os.WriteFile(plugin, []byte("#!/bin/sh\n: > '"+f.marker+"'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := "order = [\"probe\", \"env\"]\n[backend.probe]\ntype = \"external\"\ncommand = \"" + plugin + "\"\n[backend.env]\ntype = \"env\"\n[alias]\n" + aliases
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	var err error
	f.receiver, err = age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.issuer = key
	f.trust = recipientattest.Trust{Version: 1, Issuer: "https://synthetic.invalid/issuer", PublicKey: base64.StdEncoding.EncodeToString(pub), Environment: "synthetic-env"}
	f.trustPath = filepath.Join(f.dir, "trust.json")
	data, _ := json.Marshal(f.trust)
	if err := os.WriteFile(f.trustPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f aliasAttestationFixture) proof(t *testing.T, canonical string, attempt int) (recipientattest.Request, string) {
	t.Helper()
	r, err := recipientattest.CreateChallenge(f.home, f.trust, f.receiver.Recipient().String(), f.trust.Environment, canonical, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(r)
	sig := ed25519.Sign(f.issuer, append([]byte(recipientattest.SigningDomain), payload...))
	envelope, _ := json.Marshal(recipientattest.Envelope{Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(sig)})
	path := filepath.Join(f.dir, fmt.Sprintf("proof-%d.json", attempt))
	if err := os.WriteFile(path, envelope, 0o600); err != nil {
		t.Fatal(err)
	}
	return r, path
}

func (f aliasAttestationFixture) exportArgs(input, proof, nonce, output string) []string {
	return []string{"export", input, "--to", f.receiver.Recipient().String(), "--output-file", output, "--recipient-attestation", proof, "--attestation-trust", f.trustPath, "--attestation-challenge", nonce}
}

func TestAttestedPathsRejectAmbiguousAliasBeforeBackendOrConsumption(t *testing.T) {
	for name, tc := range map[string]struct{ aliases, input string }{
		"duplicate alias":       {"alpha = [\"shared\"]\nbeta = [\"shared\"]\n", "shared"},
		"case folded alias":     {"alpha = [\"SHARED\"]\nbeta = [\"shared\"]\n", "shared"},
		"canonical shadow":      {"alpha = [\"beta\"]\nbeta = [\"other\"]\n", "alpha"},
		"case folded canonical": {"alpha = [\"first\"]\nALPHA = [\"second\"]\n", "first"},
		"ambiguous fallback":    {"alpha = [\"unique\", \"shared\"]\nbeta = [\"shared\"]\n", "unique"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAliasAttestationFixture(t, tc.aliases)
			env := []string{"HOME=" + f.home, "ALPHA=synthetic-alpha", "BETA=synthetic-beta"}
			challengeOutput := filepath.Join(f.dir, "request.json")
			code, stdout, stderr := runKCSplit(env, "", "recipient-challenge", "--attestation-trust", f.trustPath, "--to", f.receiver.Recipient().String(), "--environment", f.trust.Environment, "--name", tc.input, "--output-file", challengeOutput)
			if code == 0 || stdout != "" || !strings.Contains(stderr, "ambiguous attested alias configuration") {
				t.Fatal("ambiguous challenge was not rejected")
			}
			if _, err := os.Stat(challengeOutput); !os.IsNotExist(err) {
				t.Fatal("ambiguous challenge produced output")
			}
			if _, err := os.Stat(filepath.Join(f.home, ".kc")); !os.IsNotExist(err) {
				t.Fatal("ambiguous challenge created sender state")
			}
			// A previously issued valid proof cannot be used after the local alias
			// namespace becomes ambiguous. Repeated fresh nonces must all fail early.
			for attempt := 0; attempt < 8; attempt++ {
				request, proof := f.proof(t, "alpha", attempt)
				output := filepath.Join(f.dir, fmt.Sprintf("bundle-%d.age", attempt))
				code, stdout, stderr := runKCSplit(env, "", f.exportArgs(tc.input, proof, request.Nonce, output)...)
				if code == 0 || stdout != "" || !strings.Contains(stderr, "ambiguous attested alias configuration") {
					t.Fatal("ambiguous export was not rejected")
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("ambiguous export produced a bundle")
				}
				if _, err := os.Stat(filepath.Join(f.home, ".kc", "recipient-challenges", request.Nonce+".used")); !os.IsNotExist(err) {
					t.Fatal("alias rejection consumed the proof")
				}
			}
			if _, err := os.Stat(f.marker); !os.IsNotExist(err) {
				t.Fatal("backend started before ambiguous alias rejection")
			}
		})
	}
}

func TestAttestedAliasUsesOneCanonicalForProofLookupAndBundle(t *testing.T) {
	f := newAliasAttestationFixture(t, "alpha = [\"unique\", \"UNIQUE\"]\nbeta = [\"other\"]\n")
	// Only the unambiguous fallback alias has the intended value. The other
	// canonical value must never enter a bundle authorized for alpha.
	env := []string{"HOME=" + f.home, "ALPHA=", "UNIQUE=synthetic-alpha", "BETA=synthetic-beta"}
	challengeOutput := filepath.Join(f.dir, "alias-request.json")
	code, stdout, stderr := runKCSplit(env, "", "recipient-challenge", "--attestation-trust", f.trustPath, "--to", f.receiver.Recipient().String(), "--environment", f.trust.Environment, "--name", "UNIQUE", "--output-file", challengeOutput)
	if code != 0 || stdout != "" {
		t.Fatalf("unambiguous alias challenge failed: %d %s", code, stderr)
	}
	data, err := os.ReadFile(challengeOutput)
	if err != nil {
		t.Fatal(err)
	}
	var challenge recipientattest.Request
	if err := json.Unmarshal(data, &challenge); err != nil || challenge.SecretName != "alpha" {
		t.Fatal("challenge did not bind the canonical item")
	}
	if _, err := os.Stat(f.marker); !os.IsNotExist(err) {
		t.Fatal("challenge started a backend")
	}
	for attempt := 0; attempt < 16; attempt++ {
		request, proof := f.proof(t, "alpha", attempt)
		output := filepath.Join(f.dir, fmt.Sprintf("bundle-%d.age", attempt))
		input := "unique"
		if attempt%2 != 0 {
			input = " UNIQUE "
		}
		code, stdout, stderr := runKCSplit(env, "", f.exportArgs(input, proof, request.Nonce, output)...)
		if code != 0 || stdout != "" {
			t.Fatalf("unambiguous alias export failed: %d %s", code, stderr)
		}
		ciphertext, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		secrets, err := agebundle.Decrypt(bytes.NewReader(ciphertext), f.receiver)
		if err != nil || len(secrets) != 1 || string(secrets["alpha"]) != "synthetic-alpha" {
			t.Fatal("proof, resolved value and bundle name diverged")
		}
	}
}
