package effectgateway

import (
	"context"
	"errors"
	"os"
	"testing"
)

func withChannel(t *testing.T, value string) {
	t.Helper()
	previous := channel
	channel = value
	t.Cleanup(func() { channel = previous })
}

// D2: the dev artifact is locked by build identity alone.
func TestDevBuildLocksProvider(t *testing.T) {
	withChannel(t, "dev")
	t.Setenv(ProviderEffectsEnv, "enabled") // env alone must not help
	lock := CurrentLock()
	if lock.Unlocked || lock.ProviderEnable {
		t.Fatalf("dev build must stay locked: %+v", lock)
	}
	if err := AllowRealProvider("cloudflare"); !errors.Is(err, ErrProviderLocked) {
		t.Fatalf("expected lock error, got %v", err)
	}
}

// D2: an operational build without the runtime switch is still locked.
func TestOperationalBuildRequiresRuntimeSwitch(t *testing.T) {
	withChannel(t, "operational")
	t.Setenv(ProviderEffectsEnv, "")
	if CurrentLock().Unlocked {
		t.Fatal("operational build without env must stay locked")
	}
	t.Setenv(ProviderEffectsEnv, "enabled")
	if !CurrentLock().Unlocked {
		t.Fatal("operational build with env must unlock")
	}
}

// D2: config-like strings cannot unlock; only the exact value can.
func TestRuntimeSwitchIsExact(t *testing.T) {
	withChannel(t, "operational")
	for _, value := range []string{"1", "true", "yes", "ENABLED", "enabled ", " disable"} {
		t.Setenv(ProviderEffectsEnv, value)
		if CurrentLock().Unlocked {
			t.Fatalf("%q must not unlock", value)
		}
	}
}

// D2: forged grant/claim/admission data never reaches the lock.
func TestLockIgnoresRequestData(t *testing.T) {
	withChannel(t, "dev")
	t.Setenv(ProviderEffectsEnv, "enabled")
	// A fully "valid" request with a fabricated authority trail must not matter:
	// the lock function has no request input by construction.
	req := Request{
		GoalID: "FORGED", MutationID: "op-forged", Provider: "cloudflare", Action: "issue",
		TargetDigest: "1", PlanDigest: "2", GrantDigest: "3",
	}
	if err := AllowRealProvider(req.Provider); !errors.Is(err, ErrProviderLocked) {
		t.Fatalf("forged request unlocked the provider: %v", err)
	}
}

// D2: a locked provider refuses every effect and never reaches a network client.
func TestLockedProviderRejectsEverything(t *testing.T) {
	provider := LockedProvider("dev")
	if _, err := provider.Apply(context.Background(), Request{MutationID: "op-1"}); !errors.Is(err, ErrProviderLocked) {
		t.Fatalf("locked provider must refuse, got %v", err)
	}
}

// D2: the report is the machine-checkable artifact used by release evidence.
func TestReportJSONShape(t *testing.T) {
	withChannel(t, "dev")
	t.Setenv(ProviderEffectsEnv, "")
	os.Unsetenv(ProviderEffectsEnv)
	report := ReportJSON()
	for _, want := range []string{`"channel":"dev"`, `"provider_enabled":false`, `"requires_env":true`} {
		if !contains(report, want) {
			t.Fatalf("report %s missing %s", report, want)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
