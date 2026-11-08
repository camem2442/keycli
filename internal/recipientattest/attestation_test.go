package recipientattest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// All keys and signatures in these tests are freshly generated synthetic data.
const testRecipient = "age12g3et0a64ewq78377l48p065648pcs74gw8chkz7fpu8k9te038qkyu2wu"

type fixture struct {
	home    string
	trust   Trust
	private ed25519.PrivateKey
	request Request
	clock   time.Time
}

func setup(t *testing.T) fixture {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{home: t.TempDir(), private: priv, trust: Trust{Version: Version, Issuer: "https://synthetic.invalid/issuer", PublicKey: base64.StdEncoding.EncodeToString(pub), Environment: "synthetic-environment"}, clock: time.Unix(2000000000, 0)}
	f.request, err = CreateChallenge(f.home, f.trust, testRecipient, f.trust.Environment, "demo", f.clock)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func signed(t *testing.T, private ed25519.PrivateKey, r Request) []byte {
	t.Helper()
	payload, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return signedBytes(t, private, payload)
}
func signedBytes(t *testing.T, private ed25519.PrivateKey, payload []byte) []byte {
	t.Helper()
	sig := ed25519.Sign(private, append([]byte(SigningDomain), payload...))
	data, err := json.Marshal(Envelope{Payload: base64.StdEncoding.EncodeToString(payload), Signature: base64.StdEncoding.EncodeToString(sig)})
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func (f fixture) verify(data []byte) error {
	return VerifyAndConsume(f.home, f.trust, data, f.request.Nonce, testRecipient, "demo", func() time.Time { return f.clock })
}
func (f fixture) statePath(ext string) string {
	return filepath.Join(f.home, ".kc", "recipient-challenges", f.request.Nonce+ext)
}

func TestTrustedAttestationAndReplay(t *testing.T) {
	f := setup(t)
	data := signed(t, f.private, f.request)
	if err := f.verify(data); err != nil {
		t.Fatal(err)
	}
	if err := f.verify(data); err == nil {
		t.Fatal("replay accepted")
	}
	st, err := os.Stat(f.statePath(".used"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatal("private durable marker missing")
	}
	if err := WriteRequest(f.statePath(".json"), f.request); err == nil {
		t.Fatal("request overwritten")
	}
}

func TestRejectClaimBindingsAndTime(t *testing.T) {
	cases := map[string]func(*Request){
		"issuer": func(r *Request) { r.Issuer = "other" }, "key": func(r *Request) { r.KeyID = strings.Repeat("0", 64) },
		"environment": func(r *Request) { r.Environment = "other" }, "recipient": func(r *Request) { r.Recipient = "age1other" },
		"purpose": func(r *Request) { r.Purpose = "other" }, "name": func(r *Request) { r.SecretName = "other" },
		"nonce": func(r *Request) { r.Nonce = base64.RawURLEncoding.EncodeToString(make([]byte, 32)) }, "version": func(r *Request) { r.Version = 2 },
		"future": func(r *Request) { r.IssuedAt++ }, "before-challenge": func(r *Request) { r.IssuedAt-- },
		"expired": func(r *Request) { r.ExpiresAt = r.IssuedAt }, "extension": func(r *Request) { r.ExpiresAt++ },
		"negative": func(r *Request) { r.IssuedAt = -1 }, "overflow": func(r *Request) { r.ExpiresAt = 1<<63 - 1 },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			r := f.request
			change(&r)
			if f.verify(signed(t, f.private, r)) == nil {
				t.Fatal("invalid claim accepted")
			}
			if _, err := os.Stat(f.statePath(".used")); !os.IsNotExist(err) {
				t.Fatal("invalid claim consumed challenge")
			}
		})
	}
}

func TestNoImplicitTrustOrSelfAttestation(t *testing.T) {
	f := setup(t)
	_, attacker, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if f.verify(signed(t, attacker, f.request)) == nil {
		t.Fatal("untrusted/self-signed issuer accepted")
	}
	data := signed(t, f.private, f.request)
	f.trust.PublicKey = base64.StdEncoding.EncodeToString(attacker.Public().(ed25519.PublicKey))
	if f.verify(data) == nil {
		t.Fatal("trust substitution accepted")
	}
	// Even a valid signature by a newly selected key cannot replace the key
	// pinned when the sender created this challenge.
	f.request.KeyID = keyID(attacker.Public().(ed25519.PublicKey))
	if f.verify(signed(t, attacker, f.request)) == nil {
		t.Fatal("challenge trust pin replaced")
	}
	f.trust = Trust{}
	if f.verify(data) == nil {
		t.Fatal("missing trust accepted")
	}
}

func TestStrictProtocolParsing(t *testing.T) {
	cases := map[string]func([]byte) []byte{
		"duplicate":    func(p []byte) []byte { return append([]byte(`{"version":1,`), p[1:]...) },
		"unknown":      func(p []byte) []byte { return append([]byte(`{"unknown":1,`), p[1:]...) },
		"case":         func(p []byte) []byte { return []byte(strings.Replace(string(p), `"version"`, `"Version"`, 1)) },
		"trailing":     func(p []byte) []byte { return append(p, []byte(` {}`)...) },
		"noncanonical": func(p []byte) []byte { return append([]byte(" "), p...) },
		"null":         func(p []byte) []byte { return []byte("null") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			p, _ := json.Marshal(f.request)
			if f.verify(signedBytes(t, f.private, change(p))) == nil {
				t.Fatal("noncanonical claim accepted")
			}
		})
	}
	f := setup(t)
	data := signed(t, f.private, f.request)
	for _, bad := range [][]byte{[]byte(`{}`), []byte(`null`), append(data, []byte(` {}`)...), append([]byte(`{"payload":"x",`), data[1:]...), []byte(strings.Repeat("x", maxFileBytes+1))} {
		if f.verify(bad) == nil {
			t.Fatal("malformed envelope accepted")
		}
	}
	var env Envelope
	_ = json.Unmarshal(data, &env)
	env.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	bad, _ := json.Marshal(env)
	if f.verify(bad) == nil {
		t.Fatal("bad signature accepted")
	}
}

func TestSenderExpectationsAndWindow(t *testing.T) {
	t.Run("wrong command recipient", func(t *testing.T) {
		f := setup(t)
		if VerifyAndConsume(f.home, f.trust, signed(t, f.private, f.request), f.request.Nonce, "other", "demo", func() time.Time { return f.clock }) == nil {
			t.Fatal("wrong expected recipient accepted")
		}
	})
	t.Run("wrong command name", func(t *testing.T) {
		f := setup(t)
		if VerifyAndConsume(f.home, f.trust, signed(t, f.private, f.request), f.request.Nonce, testRecipient, "other", func() time.Time { return f.clock }) == nil {
			t.Fatal("wrong expected name accepted")
		}
	})
	t.Run("missing challenge", func(t *testing.T) {
		f := setup(t)
		if err := os.Rename(f.statePath(".json"), f.statePath(".saved")); err != nil {
			t.Fatal(err)
		}
		if f.verify(signed(t, f.private, f.request)) == nil {
			t.Fatal("missing local challenge accepted")
		}
	})
	t.Run("expired sender", func(t *testing.T) {
		f := setup(t)
		f.clock = f.clock.Add(MaxLifetime)
		if f.verify(signed(t, f.private, f.request)) == nil {
			t.Fatal("expired challenge accepted")
		}
	})
	t.Run("clock rollback", func(t *testing.T) {
		f := setup(t)
		f.clock = f.clock.Add(-time.Second)
		if f.verify(signed(t, f.private, f.request)) == nil {
			t.Fatal("future challenge accepted")
		}
	})
	t.Run("expiry during consume", func(t *testing.T) {
		f := setup(t)
		calls := 0
		err := VerifyAndConsume(f.home, f.trust, signed(t, f.private, f.request), f.request.Nonce, testRecipient, "demo", func() time.Time {
			calls++
			if calls > 1 {
				return f.clock.Add(MaxLifetime)
			}
			return f.clock
		})
		if err == nil {
			t.Fatal("expired authorization returned")
		}
		if _, err := os.Stat(f.statePath(".used")); err != nil {
			t.Fatal("consumed marker not preserved")
		}
	})
	t.Run("narrowed times", func(t *testing.T) {
		f := setup(t)
		r := f.request
		r.IssuedAt++
		r.ExpiresAt--
		f.clock = f.clock.Add(time.Second)
		if err := f.verify(signed(t, f.private, r)); err != nil {
			t.Fatal(err)
		}
	})
	for _, nonce := range []string{"", "../escape", strings.Repeat("a", 42), strings.Repeat("a", 44)} {
		t.Run("nonce "+nonce, func(t *testing.T) {
			f := setup(t)
			if VerifyAndConsume(f.home, f.trust, signed(t, f.private, f.request), nonce, testRecipient, "demo", func() time.Time { return f.clock }) == nil {
				t.Fatal("invalid nonce accepted")
			}
		})
	}
}

func TestConcurrentReplayHasOneWinner(t *testing.T) {
	f := setup(t)
	data := signed(t, f.private, f.request)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- f.verify(data) }()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("expected one winner, got %d", wins)
	}
}

func TestStateAndInputSafety(t *testing.T) {
	t.Run("state directory symlink", func(t *testing.T) {
		f := setup(t)
		dir := filepath.Dir(f.statePath(".json"))
		if err := os.Rename(dir, dir+"-saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dir+"-saved", dir); err != nil {
			t.Fatal(err)
		}
		if f.verify(signed(t, f.private, f.request)) == nil {
			t.Fatal("symlink accepted")
		}
	})
	t.Run("state readable by others", func(t *testing.T) {
		f := setup(t)
		if err := os.Chmod(filepath.Dir(f.statePath(".json")), 0o755); err != nil {
			t.Fatal(err)
		}
		if f.verify(signed(t, f.private, f.request)) == nil {
			t.Fatal("public state directory accepted")
		}
	})
	t.Run("request symlink", func(t *testing.T) {
		f := setup(t)
		p := f.statePath(".json")
		if err := os.Rename(p, p+"-saved"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(p+"-saved", p); err != nil {
			t.Fatal(err)
		}
		if f.verify(signed(t, f.private, f.request)) == nil {
			t.Fatal("symlink request accepted")
		}
	})
	t.Run("used marker symlink", func(t *testing.T) {
		f := setup(t)
		if err := os.Symlink(f.statePath(".json"), f.statePath(".used")); err != nil {
			t.Fatal(err)
		}
		if f.verify(signed(t, f.private, f.request)) == nil {
			t.Fatal("symlink marker accepted")
		}
	})
	t.Run("unwritable replay path", func(t *testing.T) {
		f := setup(t)
		if err := os.Mkdir(f.statePath(".used"), 0o700); err != nil {
			t.Fatal(err)
		}
		if f.verify(signed(t, f.private, f.request)) == nil {
			t.Fatal("marker failure accepted")
		}
	})
	t.Run("read limits and trust JSON", func(t *testing.T) {
		f := setup(t)
		p := filepath.Join(t.TempDir(), "trust.json")
		d, _ := json.Marshal(f.trust)
		if err := os.WriteFile(p, d, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadTrust(p); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, 0o666); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadFile(p); err == nil {
			t.Fatal("writable trust file accepted")
		}
		_ = os.Chmod(p, 0o600)
		_ = os.WriteFile(p, []byte(strings.Repeat("x", maxFileBytes+1)), 0o600)
		if _, err := ReadFile(p); err == nil {
			t.Fatal("oversize accepted")
		}
	})
}

func TestReplayAcrossProcesses(t *testing.T) {
	f := setup(t)
	dir := t.TempDir()
	trustPath := filepath.Join(dir, "trust.json")
	envelopePath := filepath.Join(dir, "envelope.json")
	data, _ := json.Marshal(f.trust)
	if err := os.WriteFile(trustPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envelopePath, signed(t, f.private, f.request), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestReplayWorker$")
			cmd.Env = []string{"KC_ATTEST_TEST_HELPER=1", "HOME=" + f.home, "TEST_TRUST=" + trustPath, "TEST_ENVELOPE=" + envelopePath, "TEST_NONCE=" + f.request.Nonce}
			results <- cmd.Run()
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		if err == nil {
			wins++
			continue
		}
		if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 3 {
			t.Fatalf("unexpected worker error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("expected one process winner, got %d", wins)
	}
}

func TestReplayWorker(t *testing.T) {
	if os.Getenv("KC_ATTEST_TEST_HELPER") != "1" {
		t.Skip("subprocess helper")
	}
	trust, err := LoadTrust(os.Getenv("TEST_TRUST"))
	if err != nil {
		os.Exit(4)
	}
	data, err := ReadFile(os.Getenv("TEST_ENVELOPE"))
	if err != nil {
		os.Exit(4)
	}
	if VerifyAndConsume(os.Getenv("HOME"), trust, data, os.Getenv("TEST_NONCE"), testRecipient, "demo", func() time.Time { return time.Unix(2000000000, 0) }) != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
