package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/middleware"
)

var ErrUnknownProfile = errors.New("unknown delivery profile")

type MetadataSnapshot struct {
	aliases  config.Config
	evaluate *middleware.Policy
	policy   PolicyMetadata
}

type PolicyMetadata struct {
	Agent    string `json:"agent"`
	Digest   string `json:"digest"`
	Enforced bool   `json:"enforced"`
	Source   string `json:"source"`
}

// NewMetadataSnapshot reads no credential values and retains only the aliases
// and effective policy identity needed by a plan.
func NewMetadataSnapshot(cfg config.Config, agent string) (MetadataSnapshot, error) {
	if !labelPattern.MatchString(agent) {
		return MetadataSnapshot{}, errors.New("invalid delivery policy agent")
	}
	digest, err := middleware.EffectivePolicyDigest(cfg, agent)
	if err != nil {
		return MetadataSnapshot{}, errors.New("invalid KeyCLI policy or alias configuration")
	}
	rules, source := middleware.EffectiveRules(cfg, agent)
	var evaluate *middleware.Policy
	enforced := middleware.PolicyEnabled(cfg)
	if enforced && source != "none" {
		evaluate = middleware.NewPolicy(config.Config{
			Policy: config.PolicySettings{Agent: map[string]config.PolicyAgentSettings{
				agent: {Allow: slices.Clone(rules.Allow), Deny: slices.Clone(rules.Deny)},
			}},
		}, agent)
	}
	aliases := make(map[string][]string, len(cfg.Alias))
	for canonical, names := range cfg.Alias {
		aliases[canonical] = slices.Clone(names)
	}
	return MetadataSnapshot{
		aliases:  config.Config{Alias: aliases},
		evaluate: evaluate,
		policy: PolicyMetadata{
			Agent: agent, Digest: digest, Enforced: enforced, Source: source,
		},
	}, nil
}

type SecretRef struct {
	Alias         string `json:"alias"`
	CanonicalName string `json:"canonical_name"`
}

type TargetRef struct {
	Repository            string  `json:"repository,omitempty"`
	Ref                   string  `json:"ref,omitempty"`
	WorkflowRef           string  `json:"workflow_ref,omitempty"`
	Environment           string  `json:"environment,omitempty"`
	AccountAlias          string  `json:"account_alias,omitempty"`
	Worker                string  `json:"worker,omitempty"`
	Destination           string  `json:"destination,omitempty"`
	TenancyAlias          string  `json:"tenancy_alias,omitempty"`
	InstanceAlias         string  `json:"instance_alias,omitempty"`
	IdentityDomainAlias   string  `json:"identity_domain_alias,omitempty"`
	Scope                 string  `json:"scope,omitempty"`
	Organization          string  `json:"organization,omitempty"`
	SelectedRepositoryIDs []int64 `json:"selected_repository_ids,omitempty"`
}

type WIFRef struct {
	ProviderAlias       string `json:"provider_alias"`
	ServiceAccountAlias string `json:"service_account_alias"`
	Issuer              string `json:"issuer,omitempty"`
	Audience            string `json:"audience,omitempty"`
}

type DeliveryPlan struct {
	Version             int            `json:"v"`
	ID                  string         `json:"plan_id"`
	Profile             string         `json:"profile"`
	Provider            string         `json:"provider"`
	Effect              string         `json:"effect"`
	Target              TargetRef      `json:"target"`
	Source              *SecretRef     `json:"source,omitempty"`
	WIF                 *WIFRef        `json:"wif,omitempty"`
	KeyCLIPolicy        PolicyMetadata `json:"keycli_policy"`
	StaticValidation    string         `json:"static_validation"`
	RemoteVerification  string         `json:"remote_verification"`
	Unverified          []string       `json:"unverified"`
	Authoritative       bool           `json:"authoritative"`
	AcceptanceClaim     bool           `json:"acceptance_claim"`
	AuthorizesExecution bool           `json:"authorizes_execution"`
}

func BuildPlan(profiles Profiles, profileName string, metadata MetadataSnapshot) (DeliveryPlan, error) {
	p, ok := profiles.entries[profileName]
	if !ok {
		return DeliveryPlan{}, ErrUnknownProfile
	}
	if metadata.policy.Digest == "" {
		return DeliveryPlan{}, errors.New("delivery policy metadata is missing")
	}

	plan := DeliveryPlan{
		Version: 1, Profile: profileName, Provider: p.Provider,
		KeyCLIPolicy: metadata.policy, StaticValidation: "COMPLETE",
		RemoteVerification: "NOT VERIFIED",
		Unverified:         []string{"target_identity", "provider_capability", "execution_authority"},
	}
	switch p.Provider {
	case githubWIF:
		plan.Effect = "workload-identity-exchange"
		plan.Target = TargetRef{
			Repository: p.Repository, Ref: p.Ref, WorkflowRef: p.WorkflowRef, Environment: p.Environment,
		}
		plan.WIF = &WIFRef{
			ProviderAlias: p.WIFProviderAlias, ServiceAccountAlias: p.ServiceAccountAlias,
			Issuer: githubOIDCIssuer, Audience: p.Audience,
		}
		plan.Unverified = append(plan.Unverified, "wif_mapping")
	case githubSecret:
		plan.Effect = p.Effect
		ids := slices.Clone(p.SelectedRepositoryIDs)
		slices.Sort(ids)
		plan.Target = TargetRef{
			Scope: p.Scope, Repository: p.Repository, Environment: p.Environment,
			Organization: p.Organization, SelectedRepositoryIDs: ids, Destination: p.Destination,
		}
		plan.Unverified = append(plan.Unverified, "source_availability", "secret_write_scope")
	case cloudflareWorkers:
		plan.Effect = p.Effect
		plan.Target = TargetRef{
			AccountAlias: p.AccountAlias, Worker: p.Worker, Environment: p.Environment, Destination: p.Destination,
		}
		plan.Unverified = append(plan.Unverified, "source_availability", "active_deployment", "latest_uploaded_version")
	case ociWIF:
		plan.Effect = "workload-identity-exchange"
		plan.Target = TargetRef{
			TenancyAlias: p.TenancyAlias, InstanceAlias: p.InstanceAlias, IdentityDomainAlias: p.IdentityDomainAlias,
		}
		plan.WIF = &WIFRef{ProviderAlias: p.WIFProviderAlias, ServiceAccountAlias: p.ServiceAccountAlias}
		plan.Unverified = append(plan.Unverified, "issuer", "audience", "wif_mapping")
	}
	if p.Source != "" {
		canonical := metadata.aliases.CanonicalName(p.Source)
		if !labelPattern.MatchString(canonical) {
			return DeliveryPlan{}, errors.New("invalid canonical delivery source name")
		}
		if metadata.evaluate != nil && !metadata.evaluate.Allowed(canonical) {
			return DeliveryPlan{}, errors.New("delivery source denied by KeyCLI policy")
		}
		plan.Source = &SecretRef{Alias: p.Source, CanonicalName: canonical}
	}
	// Hash the versioned projection with plan_id empty. Its fields contain
	// no time, local path, or mutable provider observation.
	data, err := json.Marshal(plan)
	if err != nil {
		return DeliveryPlan{}, fmt.Errorf("encode delivery plan identity: %w", err)
	}
	digest := sha256.Sum256(data)
	plan.ID = hex.EncodeToString(digest[:])
	return plan, nil
}
