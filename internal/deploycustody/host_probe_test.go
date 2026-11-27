package deploycustody

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProbeEnvironmentIsNotCredentialAccess(t *testing.T) {
	secret := "EXAMPLE_NEVER_OUTPUT_SECRET"
	result := ProbeEnvironment(func(name string) (string, bool) {
		if name == "CLOUDFLARE_API_TOKEN" {
			return secret, true
		}
		return "", false
	})
	raw, _ := json.Marshal(result)
	if strings.Contains(string(raw), secret) || result.AuthorizesDeploy || result.AuthorizesRecovery || result.AuthorizesCredentialRead {
		t.Fatal("credential leaked or permission fabricated")
	}
	if result.Status != "HOLD_ENV_CREDENTIAL_SURFACE_PRESENT" || len(result.CredentialEnvNames) != 1 || result.CredentialEnvNames[0] != "CLOUDFLARE_API_TOKEN" {
		t.Fatal(result)
	}
}

func TestNoEnvironmentIsStillUnknownHost(t *testing.T) {
	r := ProbeEnvironment(func(string) (string, bool) { return "", false })
	if r.Status != "HOLD_HOST_CUSTODY_UNVERIFIED" || r.LocalSecretStores != "NOT_INSPECTED" || r.ClaimStore != "NOT_AUTHENTICATED" || r.AuthorizesDeploy {
		t.Fatal(r)
	}
}

func TestNilLookupIsAlwaysHold(t *testing.T) {
	if r := ProbeEnvironment(nil); r.AuthorizesDeploy || r.Status != "HOLD_HOST_CUSTODY_UNVERIFIED" {
		t.Fatal(r)
	}
}

func TestMultipleVariablesOnlyRevealNames(t *testing.T) {
	names := map[string]bool{"CF_API_TOKEN": true, "CF_API_KEY": true, "OTHER_SECRET": true}
	r := ProbeEnvironment(func(name string) (string, bool) { return "never", names[name] })
	if len(r.CredentialEnvNames) != 2 || r.CredentialEnvNames[0] != "CF_API_KEY" || r.CredentialEnvNames[1] != "CF_API_TOKEN" {
		t.Fatal(r)
	}
}
