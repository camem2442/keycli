// Package recipientattest verifies an explicitly trusted issuer's statement
// about an age recipient. It does not discover trust or attest a host itself.
package recipientattest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	Version = 1
	Purpose = "keycli.export.single.v1"
	// SigningDomain is prepended to the exact canonical payload bytes.
	SigningDomain = "keycli.recipient-attestation.v1\x00"
	MaxLifetime   = 5 * time.Minute
	maxFileBytes  = 16 << 10
)

// Trust is provisioned on the sender through an independently authenticated
// issuer/admin channel, never from the recipient's request or envelope.
// No key in an envelope or automatic network lookup can replace PublicKey.
type Trust struct {
	Version     int    `json:"version"`
	Issuer      string `json:"issuer"`
	PublicKey   string `json:"public_key"`  // standard padded base64 Ed25519 key
	Environment string `json:"environment"` // exact authorized environment ID
}

// Request is sender-owned state and the public challenge sent to the issuer.
// The issuer signs a canonical Request after authenticating the environment
// and its ownership of Recipient. Times may be narrowed, never extended.
type Request struct {
	Version     int    `json:"version"`
	Issuer      string `json:"issuer"`
	KeyID       string `json:"key_id"` // lowercase SHA-256 hex of the trusted key
	Environment string `json:"environment"`
	Recipient   string `json:"recipient"`
	Purpose     string `json:"purpose"`
	SecretName  string `json:"secret_name"` // sender's canonical name
	Nonce       string `json:"nonce"`       // 32 random bytes, raw URL base64
	IssuedAt    int64  `json:"issued_at"`   // Unix seconds
	ExpiresAt   int64  `json:"expires_at"`
}

// Envelope contains no trusted key. Payload is standard padded base64 of the
// exact compact JSON produced by json.Marshal(Request), in field order above.
type Envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"` // Ed25519 over SigningDomain || payload
}

func (t Trust) key() (ed25519.PublicKey, error) {
	k, err := base64.StdEncoding.Strict().DecodeString(t.PublicKey)
	if err != nil || len(k) != ed25519.PublicKeySize || base64.StdEncoding.EncodeToString(k) != t.PublicKey || t.Version != Version || !label(t.Issuer) || !label(t.Environment) {
		return nil, errors.New("invalid recipient attestation trust configuration")
	}
	return ed25519.PublicKey(k), nil
}

func label(s string) bool {
	if s == "" || len(s) > 1024 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func keyID(key ed25519.PublicKey) string { h := sha256.Sum256(key); return hex.EncodeToString(h[:]) }

// ReadFile rejects links, special files, oversize input and writable-by-others
// configuration. A root/current-user owner and trusted parent directories are
// required; it is not a defense against a compromised sender UID or root.
func ReadFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.New("cannot open attestation input as a regular file")
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0o022 != 0 || st.Size() > maxFileBytes {
		return nil, errors.New("unsafe attestation input file")
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || (owner.Uid != uint32(os.Geteuid()) && owner.Uid != 0) {
		return nil, errors.New("untrusted attestation input owner")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil || len(data) > maxFileBytes {
		return nil, errors.New("cannot read bounded attestation input")
	}
	return data, nil
}

func LoadTrust(path string) (Trust, error) {
	var t Trust
	data, err := ReadFile(path)
	if err != nil {
		return t, err
	}
	if err = decodeStrict(data, &t); err != nil {
		return t, errors.New("invalid recipient attestation trust JSON")
	}
	_, err = t.key()
	return t, err
}

// decodeStrict rejects duplicate, unknown and incorrectly cased field names,
// trailing JSON and null objects. All protocol objects have scalar fields.
func decodeStrict(data []byte, dst any) error {
	if len(data) > maxFileBytes {
		return errors.New("JSON size limit")
	}
	allowedBytes, err := json.Marshal(dst)
	if err != nil {
		return err
	}
	var allowed map[string]json.RawMessage
	if err = json.Unmarshal(allowedBytes, &allowed); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	tok, err := d.Token()
	if err != nil || tok != json.Delim('{') {
		return errors.New("expected JSON object")
	}
	seen := map[string]bool{}
	for d.More() {
		tok, err = d.Token()
		if err != nil {
			return err
		}
		name, ok := tok.(string)
		if !ok || seen[name] {
			return errors.New("duplicate JSON field")
		}
		if _, ok = allowed[name]; !ok {
			return errors.New("unknown JSON field")
		}
		seen[name] = true
		var raw json.RawMessage
		if err = d.Decode(&raw); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("null JSON field")
		}
	}
	if _, err = d.Token(); err != nil {
		return err
	}
	if _, err = d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	if len(seen) != len(allowed) {
		return errors.New("missing JSON field")
	}
	return json.Unmarshal(data, dst)
}

func stateDir(home string, create bool) (string, error) {
	if !filepath.IsAbs(home) {
		return "", errors.New("absolute sender home required")
	}
	p := home
	for _, name := range []string{".kc", "recipient-challenges"} {
		p = filepath.Join(p, name)
		if create {
			if err := os.Mkdir(p, 0o700); err != nil && !os.IsExist(err) {
				return "", errors.New("cannot create challenge state directory")
			}
		}
		st, err := os.Lstat(p)
		if err != nil || !st.IsDir() || st.Mode().Perm()&0o077 != 0 {
			return "", errors.New("challenge state requires private directories without symlinks")
		}
		owner, ok := st.Sys().(*syscall.Stat_t)
		if !ok || owner.Uid != uint32(os.Geteuid()) {
			return "", errors.New("challenge state owner mismatch")
		}
	}
	return p, nil
}

func validNonce(s string) bool {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == s
}

func fresh(r Request, now int64) bool {
	return r.Version == Version && r.Purpose == Purpose && r.IssuedAt > 0 &&
		r.IssuedAt <= now && r.ExpiresAt > now && r.ExpiresAt > r.IssuedAt &&
		r.ExpiresAt-r.IssuedAt <= int64(MaxLifetime/time.Second)
}

// CreateChallenge persists immutable sender expectations; the returned bytes
// are public. No secret store or recipient private key is accessed.
func CreateChallenge(home string, trust Trust, recipient, environment, name string, now time.Time) (Request, error) {
	var r Request
	key, err := trust.key()
	if err != nil {
		return r, err
	}
	if !label(recipient) || !label(name) || environment != trust.Environment {
		return r, errors.New("challenge binding does not match trusted environment")
	}
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		return r, errors.New("cannot generate challenge nonce")
	}
	r = Request{Version: Version, Issuer: trust.Issuer, KeyID: keyID(key), Environment: environment, Recipient: recipient, Purpose: Purpose, SecretName: name, Nonce: base64.RawURLEncoding.EncodeToString(nonce), IssuedAt: now.Unix(), ExpiresAt: now.Add(MaxLifetime).Unix()}
	if !fresh(r, now.Unix()) {
		return Request{}, errors.New("invalid challenge clock")
	}
	dir, err := stateDir(home, true)
	if err != nil {
		return Request{}, err
	}
	if err = WriteRequest(filepath.Join(dir, r.Nonce+".json"), r); err != nil {
		return Request{}, err
	}
	return r, nil
}

// WriteRequest never overwrites a path. A partial file is retained on error.
func WriteRequest(path string, r Request) error {
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return createFile(path, append(data, '\n'))
}

func createFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return errors.New("cannot create new attestation state/output file")
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot persist attestation file; retained for inspection")
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("cannot open attestation parent directory")
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		return errors.New("cannot persist attestation directory; retained for inspection")
	}
	return nil
}

// VerifyAndConsume checks the trusted signature, sender-owned fresh challenge,
// recipient/environment/purpose/name binding and one-use state before returning.
// Call this before any secret read. Failed exports after consumption need a new
// challenge; never remove a used marker to retry. now must be the sender clock.
func VerifyAndConsume(home string, trust Trust, envelope []byte, nonce, recipient, name string, now func() time.Time) error {
	key, err := trust.key()
	if err != nil {
		return err
	}
	if !validNonce(nonce) || !label(recipient) || !label(name) || now == nil {
		return errors.New("invalid attestation expectations")
	}
	dir, err := stateDir(home, false)
	if err != nil {
		return err
	}
	data, err := ReadFile(filepath.Join(dir, nonce+".json"))
	if err != nil {
		return errors.New("sender challenge not found or unsafe")
	}
	var request Request
	if decodeStrict(data, &request) != nil || request.Nonce != nonce {
		return errors.New("invalid sender challenge")
	}
	var env Envelope
	if decodeStrict(envelope, &env) != nil {
		return errors.New("invalid attestation envelope")
	}
	payload, err := base64.StdEncoding.Strict().DecodeString(env.Payload)
	if err != nil || base64.StdEncoding.EncodeToString(payload) != env.Payload {
		return errors.New("invalid attestation payload encoding")
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(env.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize || base64.StdEncoding.EncodeToString(sig) != env.Signature {
		return errors.New("invalid attestation signature encoding")
	}
	if !ed25519.Verify(key, append([]byte(SigningDomain), payload...), sig) {
		return errors.New("untrusted recipient attestation signature")
	}
	var claim Request
	if decodeStrict(payload, &claim) != nil {
		return errors.New("invalid attestation claims")
	}
	canonical, err := json.Marshal(claim)
	if err != nil || !bytes.Equal(payload, canonical) {
		return errors.New("attestation payload must use canonical JSON")
	}
	if request.Issuer != trust.Issuer || request.KeyID != keyID(key) || request.Environment != trust.Environment || request.Recipient != recipient || request.SecretName != name {
		return errors.New("sender challenge binding mismatch")
	}
	// Only claim timestamps may differ, and only within the sender's window.
	binding := claim
	binding.IssuedAt, binding.ExpiresAt = request.IssuedAt, request.ExpiresAt
	if binding != request {
		return errors.New("recipient attestation binding mismatch")
	}
	clock := now().Unix()
	if !fresh(request, clock) || !fresh(claim, clock) || claim.IssuedAt < request.IssuedAt || claim.ExpiresAt > request.ExpiresAt {
		return errors.New("recipient attestation expired or not fresh")
	}
	// O_EXCL is the cross-process winner election. State is durable before a
	// secret is read, including on failed exports; the marker contains no secret.
	if err = createFile(filepath.Join(dir, nonce+".used"), []byte("consumed\n")); err != nil {
		return fmt.Errorf("challenge already consumed or cannot persist replay protection: %w", err)
	}
	if !fresh(request, now().Unix()) || !fresh(claim, now().Unix()) {
		return errors.New("recipient attestation expired during verification; challenge consumed")
	}
	return nil
}
