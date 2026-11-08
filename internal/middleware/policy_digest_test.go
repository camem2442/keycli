package middleware_test

import (
	"regexp"
	"testing"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/middleware"
)

func TestEffectivePolicyDigestStableAcrossMapInsertionOrder(t *testing.T) {
	a := config.Default(t.TempDir())
	a.Middleware = []string{"audit", "policy"}
	a.Policy.Agent = map[string]config.PolicyAgentSettings{
		"default": {Allow: []string{}},
		"codex":   {Allow: []string{"*"}, Deny: []string{"private_*"}},
	}
	a.Alias = map[string][]string{
		"github": {"GH_TOKEN", "GITHUB_TOKEN"},
		"demo":   {"DEMO"},
	}
	b := config.Default(t.TempDir())
	b.Middleware = []string{"policy", "audit"}
	b.Policy.Agent = map[string]config.PolicyAgentSettings{
		"codex":   {Allow: []string{"*"}, Deny: []string{"private_*"}},
		"default": {Allow: []string{}},
	}
	b.Alias = map[string][]string{
		"demo":   {"DEMO"},
		"github": {"GH_TOKEN", "GITHUB_TOKEN"},
	}
	one, err := middleware.EffectivePolicyDigest(a, "codex")
	if err != nil {
		t.Fatal(err)
	}
	two, err := middleware.EffectivePolicyDigest(b, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if one != two || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(one) {
		t.Fatalf("non-deterministic policy digest: %q vs %q", one, two)
	}
}

func TestEffectivePolicyDigestTracksRelevantChanges(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Middleware = []string{"policy"}
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"default": {Allow: []string{}},
		"codex":   {Allow: []string{"*"}, Deny: []string{"private_*"}},
	}
	cfg.Alias = map[string][]string{"github": {"GH_TOKEN"}}
	original, err := middleware.EffectivePolicyDigest(cfg, "codex")
	if err != nil {
		t.Fatal(err)
	}
	assertChanged := func(label string, candidate config.Config, agent string) {
		t.Helper()
		got, err := middleware.EffectivePolicyDigest(candidate, agent)
		if err != nil || got == original {
			t.Fatalf("%s did not change digest: %q, %v", label, got, err)
		}
	}
	aliasChange := cfg
	aliasChange.Alias = map[string][]string{"github": {"NEW_GH_TOKEN"}}
	assertChanged("alias", aliasChange, "codex")

	denyChange := cfg
	denyChange.Policy.Agent = map[string]config.PolicyAgentSettings{
		"default": {Allow: []string{}},
		"codex":   {Allow: []string{"*"}, Deny: []string{"private_*", "wallet"}},
	}
	assertChanged("deny", denyChange, "codex")

	disabled := cfg
	disabled.Middleware = []string{"audit"}
	assertChanged("middleware enforcement", disabled, "codex")
	assertChanged("agent and inherited rules", cfg, "claude")
}

func TestEffectivePolicyDigestRejectsInvalidPatterns(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Middleware = []string{"policy"}
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"codex": {Allow: []string{"*"}, Deny: []string{"broken_["}},
	}
	if got, err := middleware.EffectivePolicyDigest(cfg, "codex"); err == nil || got != "" {
		t.Fatalf("invalid deny glob must not yield a digest: %q %v", got, err)
	}
}

func TestEffectivePolicyDigestEmptyListsStable(t *testing.T) {
	a := config.Default(t.TempDir())
	b := config.Default(t.TempDir())
	b.Policy.Agent = map[string]config.PolicyAgentSettings{}
	for _, agent := range []string{"codex", "human"} {
		left, err := middleware.EffectivePolicyDigest(a, agent)
		if err != nil {
			t.Fatal(err)
		}
		right, err := middleware.EffectivePolicyDigest(b, agent)
		if err != nil || left != right {
			t.Fatalf("nil vs empty policy map changed digest: %s %s %v", left, right, err)
		}
	}
}
