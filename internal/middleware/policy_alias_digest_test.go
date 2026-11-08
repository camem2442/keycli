package middleware_test

import (
	"testing"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/middleware"
)

func TestPolicyDigestRejectsAmbiguousAliasOwners(t *testing.T) {
	for _, aliases := range []map[string][]string{
		{"allowed": {"shared"}, "denied": {"shared"}},
		{"allowed": {"Shared"}, "denied": {"SHARED"}},
		{"allowed": {"denied"}, "denied": {}},
		{"Allowed": {}, "allowed": {}},
	} {
		cfg := config.Default(t.TempDir())
		cfg.Middleware = []string{"policy"}
		cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
			"codex": {Allow: []string{"allowed"}, Deny: []string{"denied"}},
		}
		cfg.Alias = aliases
		for i := 0; i < 20; i++ {
			if got, err := middleware.EffectivePolicyDigest(cfg, "codex"); err == nil || got != "" {
				t.Fatalf("ambiguous alias map received a digest: %q %v", got, err)
			}
		}
	}
}
