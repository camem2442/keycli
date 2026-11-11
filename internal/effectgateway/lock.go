package effectgateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Provider effects are locked by default. Two independent conditions must hold
// before a real provider adapter may run:
//
//  1. the binary was built with channel=operational (link-time, not request data)
//  2. KC_PROVIDER_EFFECTS=enabled is set in the host environment
//
// Neither a Grant, a Claim, a signed envelope nor a forged admission decision can
// change this: the lock depends only on build identity and process environment.
const ProviderEffectsEnv = "KC_PROVIDER_EFFECTS"

// channel is fixed at link time:
//
//	go build -ldflags "-X github.com/camem2442/keycli/internal/effectgateway.channel=operational"
//
// The zero value is the safe one.
var channel = "dev"

// ErrProviderLocked is returned when a real provider effect is attempted in a
// build or environment that is not allowed to perform one.
var ErrProviderLocked = errors.New("provider effects are locked in this artifact")

// ProviderLock is the evaluated state of the two-layer lock.
type ProviderLock struct {
	Channel        string `json:"channel"`
	Unlocked       bool   `json:"unlocked"`
	RequiresEnv    bool   `json:"requires_env"`
	ProviderEnable bool   `json:"provider_enabled"`
	Reason         string `json:"reason"`
}

// CurrentLock evaluates the lock from build identity and environment only.
func CurrentLock() ProviderLock {
	return evaluateLock(channel, os.Getenv(ProviderEffectsEnv))
}

func evaluateLock(buildChannel, env string) ProviderLock {
	lock := ProviderLock{Channel: buildChannel, RequiresEnv: true}
	if buildChannel != "operational" {
		lock.Reason = "build-channel-is-" + safeLabel(buildChannel)
		return lock
	}
	if env != "enabled" {
		lock.Reason = "runtime-env-not-enabled"
		return lock
	}
	lock.Unlocked = true
	lock.ProviderEnable = true
	lock.Reason = "operational-build-and-runtime-enabled"
	return lock
}

func safeLabel(value string) string {
	if value == "" {
		return "unset"
	}
	if len(value) > 32 {
		return "unknown"
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return "unknown"
		}
	}
	return value
}

// AllowRealProvider is the single gate a real adapter must pass before any effect.
// name is advisory; the decision never depends on caller-supplied data.
func AllowRealProvider(name string) error {
	lock := CurrentLock()
	if !lock.Unlocked {
		return fmt.Errorf("%w: %s (%s)", ErrProviderLocked, strings.TrimSpace(name), lock.Reason)
	}
	return nil
}

// ReportJSON is a compact, machine-checkable lock report for release evidence.
func ReportJSON() string {
	lock := CurrentLock()
	return fmt.Sprintf(
		`{"schema_version":1,"channel":%q,"unlocked":%t,"requires_env":%t,"provider_enabled":%t,"reason":%q}`,
		lock.Channel, lock.Unlocked, lock.RequiresEnv, lock.ProviderEnable, lock.Reason)
}

// rejectAll is the provider a locked build is wired to.
type rejectAll struct{ reason string }

func (r rejectAll) Apply(_ context.Context, req Request) (EffectResult, error) {
	return EffectResult{}, fmt.Errorf("%w: %s", ErrProviderLocked, r.reason)
}

// LockedProvider returns a provider that refuses every effect.
func LockedProvider(reason string) Provider { return rejectAll{reason: reason} }
