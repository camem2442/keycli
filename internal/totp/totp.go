// Package totp derives six-digit one-time codes from an otpauth URI.
//
// It implements the generator side of RFC 6238. The seed is a secret: no error
// returned here repeats it, and Config is not printable.
package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"time"
)

// Config is one TOTP entry.
type Config struct {
	secret    []byte
	algorithm string
	digits    int
	period    int
}

// String keeps a Config out of logs and error messages.
func (Config) String() string { return "totp.Config{redacted}" }

// GoString keeps a Config out of %#v output.
func (Config) GoString() string { return "totp.Config{redacted}" }

// Digits and Period expose the non-secret parameters.
func (c Config) Digits() int { return c.digits }

func (c Config) Period() int { return c.period }

// Algorithm exposes the HMAC algorithm name.
func (c Config) Algorithm() string { return c.algorithm }

// Parse accepts an otpauth://totp URI, including Apple's label-less spelling
// ("otpauth://totp?secret=…"), or a bare Base32 seed with the RFC defaults.
func Parse(value string) (Config, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return Config{}, errors.New("empty otp value")
	}
	if !strings.Contains(strings.ToLower(raw), "://") {
		secret, err := decodeBase32(raw)
		if err != nil {
			return Config{}, err
		}
		return Config{secret: secret, algorithm: "SHA1", digits: 6, period: 30}, nil
	}

	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "otpauth://") {
		return Config{}, errors.New("unsupported otp URI scheme")
	}
	if strings.HasPrefix(lower, "otpauth://hotp") {
		return Config{}, errors.New("HOTP is not supported; only time-based codes are")
	}
	if !strings.HasPrefix(lower, "otpauth://totp") {
		return Config{}, errors.New("unsupported otp URI type")
	}

	query := ""
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		query = raw[i+1:]
	}
	params := map[string]string{}
	for _, pair := range strings.Split(query, "&") {
		if pair == "" {
			continue
		}
		key, val, _ := strings.Cut(pair, "=")
		params[strings.ToLower(key)] = val
	}

	secretRaw := params["secret"]
	if secretRaw == "" {
		return Config{}, errors.New("otp URI has no secret parameter")
	}
	secret, err := decodeBase32(secretRaw)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{secret: secret, algorithm: "SHA1", digits: 6, period: 30}
	if alg := strings.ToUpper(params["algorithm"]); alg != "" {
		switch alg {
		case "SHA1", "SHA256", "SHA512":
			cfg.algorithm = alg
		default:
			return Config{}, fmt.Errorf("unsupported otp algorithm %q", alg)
		}
	}
	if d := params["digits"]; d != "" {
		n, err := strconv.Atoi(d)
		if err != nil || n < 6 || n > 8 {
			return Config{}, errors.New("otp digits must be 6, 7 or 8")
		}
		cfg.digits = n
	}
	if p := params["period"]; p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 300 {
			return Config{}, errors.New("otp period must be between 1 and 300 seconds")
		}
		cfg.period = n
	}
	return cfg, nil
}

// Code returns the code for the step that contains t.
func (c Config) Code(t time.Time) (string, error) {
	if c.period <= 0 || len(c.secret) == 0 {
		return "", errors.New("otp config is not initialised")
	}
	counter := t.Unix() / int64(c.period)
	if counter < 0 {
		return "", errors.New("time before the unix epoch")
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	mac := hmac.New(c.hash, c.secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	binary32 := (uint32(sum[offset]&0x7f) << 24) |
		(uint32(sum[offset+1]) << 16) |
		(uint32(sum[offset+2]) << 8) |
		uint32(sum[offset+3])
	mod := uint32(1)
	for i := 0; i < c.digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", c.digits, binary32%mod), nil
}

// Remaining reports how many seconds the current step has left.
func (c Config) Remaining(t time.Time) int {
	rem := c.period - int(t.Unix()%int64(c.period))
	if rem == c.period {
		return c.period
	}
	return rem
}

func (c Config) hash() hash.Hash {
	switch c.algorithm {
	case "SHA256":
		return sha256.New()
	case "SHA512":
		return sha512.New()
	default:
		return sha1.New()
	}
}

func decodeBase32(value string) ([]byte, error) {
	clean := strings.ToUpper(strings.Join(strings.Fields(value), ""))
	clean = strings.TrimRight(clean, "=")
	if clean == "" {
		return nil, errors.New("otp secret is empty")
	}
	// Base32 (RFC 4648) is A-Z and 2-7. Lowercase was folded above.
	for _, r := range clean {
		if !((r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7')) {
			return nil, errors.New("otp secret is not valid Base32")
		}
	}
	if pad := len(clean) % 8; pad != 0 {
		clean += strings.Repeat("=", 8-pad)
	}
	secret, err := base32.StdEncoding.DecodeString(clean)
	if err != nil {
		return nil, errors.New("otp secret is not valid Base32")
	}
	return secret, nil
}
