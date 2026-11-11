package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/delivery"
)

func TestOfflineAuthProbeNeverOpensApp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	poisonConfig := filepath.Join(home, "invalid-kc.toml")
	t.Setenv("KC_CONFIG", poisonConfig)
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(poisonConfig, "[[invalid")
	configDir := filepath.Join(home, ".config", "kc")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "delivery.toml")
	write(configPath, `schema_version = 1
[profiles.fixture]
provider = "github-actions-wif"
repository = "example/fixture"
ref = "refs/heads/main"
workflow_ref = "example/fixture/.github/workflows/probe.yml@refs/heads/main"
environment = "ci"
audience = "https://api.openai.com/v1"
wif_provider_alias = "fixture-provider"
service_account_alias = "fixture-account"

[profiles.oracle]
provider = "oci-instance-wif"
tenancy_alias = "fixture-tenancy"
instance_alias = "fixture-instance"
identity_domain_alias = "fixture-domain"
wif_provider_alias = "fixture-provider"
service_account_alias = "fixture-account"
`)
	claimsPath := filepath.Join(home, "claims.json")
	const fixture = `{
"iss":"https://token.actions.githubusercontent.com",
"aud":"https://api.openai.com/v1",
"repository":"example/fixture",
"ref":"refs/heads/main",
"workflow_ref":"example/fixture/.github/workflows/probe.yml@refs/heads/main",
"environment":"ci",
"event_name":"workflow_dispatch"
}`
	write(claimsPath, fixture)
	const marker = "fixture-marker-do-not-echo"
	mismatchPath := filepath.Join(home, marker+".json")
	write(mismatchPath, strings.Replace(fixture, "workflow_dispatch", marker, 1))
	compactPath := filepath.Join(home, "compact.txt")
	write(compactPath, marker+".fixture.signature")
	baseArgs := []string{"auth", "probe", "--profile", "fixture", "--claims", claimsPath}

	for _, tc := range []struct {
		name string
		args []string
		code int
	}{
		{"matching JSON", append(append([]string{}, baseArgs...), "--offline", "--config", configPath, "--json"), 0},
		{"matching text", append(append([]string{}, baseArgs...), "--offline"), 0},
		{"missing offline flag", baseArgs, 64},
		{"offline false", append(append([]string{}, baseArgs...), "--offline=false"), 64},
		{"missing claims", []string{"auth", "probe", "--profile", "fixture", "--offline"}, 64},
		{"unknown profile", []string{"auth", "probe", "--profile", "absent", "--offline", "--claims", claimsPath}, 64},
		{"unsupported profile", []string{"auth", "probe", "--profile", "oracle", "--offline", "--claims", claimsPath}, 64},
		{"wrong event", []string{"auth", "probe", "--profile", "fixture", "--offline", "--claims", mismatchPath}, 1},
		{"compact JWT", []string{"auth", "probe", "--profile", "fixture", "--offline", "--claims", compactPath}, 1},
		{"help", []string{"help", "auth", "probe"}, 0},
		{"typo", []string{"auth", "unknown"}, 64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newRoot(func(context.Context) (*app.App, error) {
				t.Fatal("offline auth initialized the application")
				return nil, nil
			})
			var stdout, stderr bytes.Buffer
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(tc.args)
			if code := runRoot(root); code != tc.code {
				t.Fatalf("exit %d, want %d: %s", code, tc.code, stderr.String())
			}
			allOutput := stdout.String() + stderr.String()
			if strings.Contains(allOutput, marker) || strings.Contains(allOutput, "example/fixture") {
				t.Fatal("fixture data leaked in probe output")
			}
			switch tc.name {
			case "matching JSON":
				var result delivery.GitHubClaimProbe
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if !result.ClaimsMatched || result.Mode != "offline-synthetic" || result.Authentication != "NOT VERIFIED" ||
					result.Authoritative || result.AcceptanceClaim || result.AuthorizesExecution {
					t.Fatal("probe result did not preserve the advisory boundary")
				}
			case "matching text":
				if !strings.Contains(allOutput, "Authentication: NOT VERIFIED") {
					t.Fatal("human output omitted the authentication boundary")
				}
			}
		})
	}
	if _, err := os.Stat(filepath.Join(home, ".kc")); !os.IsNotExist(err) {
		t.Fatalf("auth probe touched the KeyCLI store: %v", err)
	}
}
