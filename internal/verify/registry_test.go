package verify_test

import (
	"testing"

	"github.com/camem2442/keycli/internal/verify"
)

func TestDetectProvider(t *testing.T) {
	cases := []struct {
		name, note string
		want       verify.Provider
		ok         bool
	}{
		{"cloudflare_agent", "", verify.ProviderCloudflareAgent, true},
		{"other", "cloudflare_billing_read", verify.ProviderCloudflareBillingRead, true},
		{"cloudflare_billing_read", "", verify.ProviderCloudflareBillingRead, true},
		{"github_agent", "", verify.ProviderGitHubAgent, true},
		{"github", "", "", false},
		{"github_pat", "", verify.ProviderGitHubPAT, true},
	}
	for _, tc := range cases {
		p, ok := verify.DetectProvider(tc.name, tc.note)
		if ok != tc.ok || p != tc.want {
			t.Fatalf("%q/%q: got %q %v want %q %v", tc.name, tc.note, p, ok, tc.want, tc.ok)
		}
	}
}
