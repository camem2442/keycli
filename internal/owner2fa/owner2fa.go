// Package owner2fa contains owner-only TOTP fulfillment logic. It is not a
// remote tool, an MCP method, a password manager, or an Apple passkey API.
// Only an independently authorized, credential-isolated browser worker may
// instantiate these interfaces. In particular, a same-UID agent on macOS is
// NOT isolated by this package.
package owner2fa

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

// Status is a fixed and safe result. It never includes a one-time code.
type Status string

const (
	Filled      Status = "filled_by_owner"
	HumanNeeded Status = "human_action_required"
)

var (
	ErrDenied     = errors.New("owner2fa: denied")
	ErrCodeWindow = errors.New("owner2fa: code window too short")
)

// Factors are supplied by the trusted browser after inspecting an actual
// on-origin challenge. Neither the factor nor the site is an agent assertion.
type Factor string

const (
	BrokerTOTP   Factor = "broker_totp"
	AppleTOTP    Factor = "apple_autofill_totp"
	Passkey      Factor = "apple_passkey"
	SMS          Factor = "sms_otp"
	Email        Factor = "email_otp"
	Push         Factor = "push_approval"
	RecoveryCode Factor = "recovery_code"
)

type Challenge struct {
	Site      string
	RequestID string
	Factor    Factor
	ExpiresAt time.Time
}

// Authority MUST query original Goal/Grant/Claim and atomic replay journal
// via a trusted owner-host connection. No caller-provided boolean is proof.
type Authority interface {
	VerifyAndReserve(context.Context, Challenge) error
}

// SeedProvider is a separate owner-only authenticator store, not kc get, kc
// exec, a model tool, a CSV exporter or the Apple Passwords database.
type SeedProvider interface {
	SeedForSite(context.Context, string) ([]byte, error)
}

// Filler is an owner-host browser interface, not arbitrary page JS. It MUST
// check current top-level HTTPS origin and a live server challenge again.
type Filler interface {
	FillVerifiedTOTP(context.Context, string, []byte) error
}

// Fulfill never returns OTP or seed to its caller. The agent only receives a
// fixed status. A browser worker MUST avoid printing/storing the code.
func Fulfill(ctx context.Context, now time.Time, c Challenge, auth Authority, seeds SeedProvider, fill Filler) (Status, error) {
	if !validSite(c.Site) || !validID(c.RequestID) || !c.ExpiresAt.After(now) || c.ExpiresAt.After(now.Add(2*time.Minute)) || auth == nil {
		return "", ErrDenied
	}
	switch c.Factor {
	case AppleTOTP, Passkey, SMS, Email, Push, RecoveryCode:
		// Apple Passwords suggestions and platform assertions require owner
		// interaction; never pretend an agent can programmatically select it.
		if auth.VerifyAndReserve(ctx, c) != nil {
			return "", ErrDenied
		}
		return HumanNeeded, nil
	case BrokerTOTP:
		if seeds == nil || fill == nil {
			return "", ErrDenied
		}
	default:
		return "", ErrDenied
	}
	if auth.VerifyAndReserve(ctx, c) != nil {
		return "", ErrDenied
	}
	if now.Unix()%30 >= 25 {
		return "", ErrCodeWindow
	}
	seed, err := seeds.SeedForSite(ctx, c.Site)
	if err != nil || len(seed) < 16 || len(seed) > 128 {
		// The returned buffer belongs exclusively to the owner broker.
		wipe(seed)
		return "", ErrDenied
	}
	defer wipe(seed)
	code := totpSHA1(seed, uint64(now.Unix()/30), 6)
	defer wipe(code)
	if fill.FillVerifiedTOTP(ctx, c.Site, code) != nil {
		// The browser may have partially acted. Never automatically retry.
		return "", ErrDenied
	}
	return Filled, nil
}

// TOTP follows RFC 6238 (SHA-1 / 30 s). This function is kept unexported;
// no CLI or MCP tool may retrieve a code.
func totpSHA1(seed []byte, counter uint64, digits int) []byte {
	var t [8]byte
	binary.BigEndian.PutUint64(t[:], counter)
	mac := hmac.New(sha1.New, seed)
	_, _ = mac.Write(t[:])
	h := mac.Sum(nil)
	off := int(h[len(h)-1] & 0x0f)
	value := (uint32(h[off])&0x7f)<<24 | uint32(h[off+1])<<16 | uint32(h[off+2])<<8 | uint32(h[off+3])
	value %= uint32(pow10(digits))
	out := make([]byte, digits)
	for i := digits - 1; i >= 0; i-- {
		out[i] = byte(value%10) + '0'
		value /= 10
	}
	wipe(h)
	return out
}

func pow10(n int) int {
	x := 1
	for i := 0; i < n; i++ {
		x *= 10
	}
	return x
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func validID(s string) bool {
	if len(s) < 8 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func validSite(site string) bool {
	if len(site) < 3 || len(site) > 253 || !strings.Contains(site, ".") || site != strings.ToLower(site) {
		return false
	}
	for _, part := range strings.Split(site, ".") {
		if len(part) == 0 || len(part) > 63 || part[0] == '-' || part[len(part)-1] == '-' {
			return false
		}
		for _, r := range part {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	return true
}
