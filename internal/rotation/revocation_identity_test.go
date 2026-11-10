package rotation

import (
	"context"
	"strings"
	"testing"
	"time"
)

func fixtureOldCredential(plan PreparationPlan, handle string) VerifiedOldCredential {
	id := VerifiedOldCredential{
		Provider: plan.Old.Provider, PlanDigest: plan.ID, PlanTarget: plan.Old.Target,
		CredentialID: handle, Generation: plan.Old.Number,
		ObservedAt: time.Now(), ProofDigest: strings.Repeat("9", 64),
	}
	switch plan.Old.Provider {
	case "github":
		id.Kind = "github-actions-secret"
		id.AccountID = "102"
		id.ResourceID = "203"
	case "cloudflare":
		id.Kind = "cloudflare-account-token"
		id.AccountID = strings.Repeat("a", 32)
		id.ResourceID = id.AccountID
	case "oracle":
		id.Kind = "oci-vault-secret-version"
		id.AccountID = "ocid1.tenancy.oc1..tenant99"
		id.ResourceID = "ocid1.vaultsecret.oc1..secret77"
	}
	return id
}
func cloudflareOldHandle() string { return strings.Repeat("1", 32) }
func fixtureIdentityVerifier(_ context.Context, p PreparationPlan, h string) (VerifiedOldCredential, error) {
	return fixtureOldCredential(p, h), nil
}

func TestExactOldCredentialProviderIdentity(t *testing.T) {
	fixtures := []struct{ name, provider, handle string }{
		{"github_secret", "github", "EXAMPLE_GEN_1"},
		{"cloudflare_api_token", "cloudflare", cloudflareOldHandle()},
		{"oci_vault_version", "oracle", "7"},
	}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			old := Generation{Kind: StaticServiceKey, Alias: "fixture", Provider: f.provider, Target: "target-one", Number: 1}
			new := old
			new.Number = 2
			plan, err := NewPreparationPlan(old, new, []Consumer{{ID: "tester", Required: true}})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			right := fixtureOldCredential(plan, f.handle)
			if err := validOldCredentialIdentity(plan, f.handle, right, now); err != nil {
				t.Fatal(err)
			}
			rejects := []struct {
				name   string
				mutate func(*VerifiedOldCredential)
			}{
				{"provider", func(x *VerifiedOldCredential) { x.Provider = "other" }},
				{"account", func(x *VerifiedOldCredential) { x.AccountID = "other" }},
				{"resource", func(x *VerifiedOldCredential) { x.ResourceID = "other" }},
				{"physical_id", func(x *VerifiedOldCredential) { x.CredentialID = "other" }},
				{"generation", func(x *VerifiedOldCredential) { x.Generation = 2 }},
				{"target", func(x *VerifiedOldCredential) { x.PlanTarget = "other" }},
				{"kind", func(x *VerifiedOldCredential) { x.Kind = "other" }},
				{"plan_digest", func(x *VerifiedOldCredential) { x.PlanDigest = strings.Repeat("1", 64) }},
				{"stale", func(x *VerifiedOldCredential) { x.ObservedAt = now.Add(-16 * time.Minute) }},
				{"missing_remote_proof", func(x *VerifiedOldCredential) { x.ProofDigest = "" }},
			}
			for _, tc := range rejects {
				t.Run(tc.name, func(t *testing.T) {
					x := right
					tc.mutate(&x)
					if validOldCredentialIdentity(plan, f.handle, x, now) == nil {
						t.Fatal("incorrect provider identity admitted")
					}
				})
			}
		})
	}
}

func TestProviderRevokerBoundaries(t *testing.T) {
	old := Generation{Alias: "fixture", Kind: StaticServiceKey, Provider: "github", Target: "repo-one", Number: 1}
	new := old
	new.Number = 2
	p, _ := NewPreparationPlan(old, new, []Consumer{{ID: "agent", Required: true}})
	if validOldCredentialIdentity(p, "SAME_NAME", fixtureOldCredential(p, "SAME_NAME"), time.Now()) == nil {
		t.Fatal("GitHub unversioned Secret can overwrite new generation")
	}
	old.Provider = "cloudflare"
	new.Provider = "cloudflare"
	p, _ = NewPreparationPlan(old, new, []Consumer{{ID: "agent", Required: true}})
	x := fixtureOldCredential(p, cloudflareOldHandle())
	x.Kind = "cloudflare-worker-secret"
	if validOldCredentialIdentity(p, x.CredentialID, x, time.Now()) == nil {
		t.Fatal("Worker secret promoted into token revocation")
	}
	old.Provider = "oracle"
	new.Provider = "oracle"
	p, _ = NewPreparationPlan(old, new, []Consumer{{ID: "agent", Required: true}})
	x = fixtureOldCredential(p, "1")
	x.CredentialID = "0"
	if validOldCredentialIdentity(p, "0", x, time.Now()) == nil {
		t.Fatal("OCI version zero admitted")
	}
}
