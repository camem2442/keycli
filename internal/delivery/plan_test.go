package delivery

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/config"
)

const cloudflareProfile = `schema_version = 1

[profiles.cloudflare_dev]
provider = "cloudflare-workers"
account_alias = "development"
worker = "kc-fixture"
environment = "staging"
source = "fixture_alias"
destination = "WORKER_CREDENTIAL"
effect = "inactive-version"
`

const otherProfiles = `
[profiles.github_openai]
provider = "github-actions-wif"
repository = "example/fixture"
ref = "refs/heads/main"
workflow_ref = "example/fixture/.github/workflows/probe.yml@refs/heads/main"
environment = "ci"
audience = "https://api.openai.com/v1"
wif_provider_alias = "github-fixture"
service_account_alias = "ci-fixture"

[profiles.oracle_ci]
provider = "oci-instance-wif"
tenancy_alias = "fixture-tenancy"
instance_alias = "fixture-instance"
identity_domain_alias = "fixture-domain"
wif_provider_alias = "oci-fixture"
service_account_alias = "oci-service"
`

func TestStrictProfiles(t *testing.T) {
	full := cloudflareProfile + otherProfiles
	cases := map[string]string{
		"unknown schema":              strings.Replace(full, "schema_version = 1", "schema_version = 2", 1),
		"unknown field":               full + "credential_value = \"metadata-fixture\"\n",
		"duplicate profile":           full + otherProfiles,
		"ambiguous profile":           full + strings.Replace(strings.TrimPrefix(cloudflareProfile, "schema_version = 1\n"), "[profiles.cloudflare_dev]", "[profiles.Cloudflare_Dev]", 1),
		"wrong scope even when empty": full + "repository = \"\"\n",
		"wrong field casing":          strings.Replace(full, "account_alias =", "Account_Alias =", 1),
		"wrong effect":                strings.Replace(full, "inactive-version", "deploy", 1),
		"missing environment":         strings.Replace(full, "environment = \"staging\"\n", "", 1),
		"wrong workflow repository":   strings.Replace(full, "fixture/.github", "other/.github", 1),
		"wrong workflow ref":          strings.Replace(full, "probe.yml@refs/heads/main", "probe.yml@refs/heads/other", 1),
		"invalid Git ref":             strings.ReplaceAll(full, "refs/heads/main", "refs/heads/.."),
		"missing WIF mapping":         strings.Replace(full, "wif_provider_alias = \"oci-fixture\"\n", "", 1),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseProfiles([]byte(input)); err == nil {
				t.Fatal("invalid profile accepted")
			} else if strings.Contains(err.Error(), "metadata-fixture") {
				t.Fatal("input value leaked in diagnostic")
			}
		})
	}
}

func TestBuildPlanMetadataAndIdentity(t *testing.T) {
	input := cloudflareProfile + otherProfiles
	profiles, err := ParseProfiles([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Alias: map[string][]string{"fixture-canonical": {"fixture_alias"}}}
	meta, err := NewMetadataSnapshot(cfg, "ci")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Alias["fixture-canonical"][0] = "mutated-alias"
	for _, name := range []string{"cloudflare_dev", "github_openai", "oracle_ci"} {
		t.Run(name, func(t *testing.T) {
			plan, err := BuildPlan(profiles, name, meta)
			if err != nil {
				t.Fatal(err)
			}
			repeat, err := BuildPlan(profiles, name, meta)
			firstJSON, _ := json.Marshal(plan)
			secondJSON, _ := json.Marshal(repeat)
			if err != nil || string(firstJSON) != string(secondJSON) || len(plan.ID) != 64 {
				t.Fatal("plan is not deterministic")
			}
			if plan.Authoritative || plan.AcceptanceClaim || plan.AuthorizesExecution || plan.RemoteVerification != "NOT VERIFIED" {
				t.Fatal("plan claimed remote evidence or authority")
			}
			if name == "cloudflare_dev" {
				if plan.Source == nil || plan.Source.CanonicalName != "fixture-canonical" || plan.Target.Destination != "WORKER_CREDENTIAL" || plan.WIF != nil {
					t.Fatal("wrong Cloudflare source or destination")
				}
			} else if plan.Source != nil || plan.WIF == nil {
				t.Fatal("wrong WIF reference")
			}
		})
	}
	original, _ := BuildPlan(profiles, "cloudflare_dev", meta)
	changed, err := ParseProfiles([]byte(strings.Replace(input, "kc-fixture", "kc-other", 1)))
	if err != nil {
		t.Fatal(err)
	}
	targetChanged, _ := BuildPlan(changed, "cloudflare_dev", meta)
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{"ci": {Allow: []string{"fixture_alias"}}}
	cfg.Middleware = []string{"policy"}
	newMeta, err := NewMetadataSnapshot(cfg, "ci")
	if err != nil {
		t.Fatal(err)
	}
	policyChanged, _ := BuildPlan(profiles, "cloudflare_dev", newMeta)
	if original.ID == targetChanged.ID || original.ID == policyChanged.ID || original.KeyCLIPolicy.Digest == policyChanged.KeyCLIPolicy.Digest {
		t.Fatal("plan identity did not bind target and policy")
	}
	cfg.Policy.Agent["ci"] = config.PolicyAgentSettings{Allow: []string{"*"}, Deny: []string{"fixture_alias"}}
	deniedMeta, err := NewMetadataSnapshot(cfg, "ci")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(profiles, "cloudflare_dev", deniedMeta); err == nil {
		t.Fatal("denied delivery source accepted")
	}
	cfg.Alias["another"] = []string{"MUTATED-ALIAS"}
	if _, err := NewMetadataSnapshot(cfg, "ci"); err == nil {
		t.Fatal("ambiguous alias accepted")
	}
}
