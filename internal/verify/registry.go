package verify

import "strings"

// Provider identifies a set-time verification implementation (v1).
type Provider string

const (
	ProviderCloudflareAgent       Provider = "cloudflare_agent"
	ProviderCloudflareBillingRead Provider = "cloudflare_billing_read"
	ProviderGitHubAgent           Provider = "github_agent"
	ProviderGitHubPAT             Provider = "github_pat"
)

// cloudflareBySuffix maps the part after "cloudflare_" to a provider.
var cloudflareBySuffix = map[string]Provider{
	"agent":        ProviderCloudflareAgent,
	"billing_read": ProviderCloudflareBillingRead,
}

// githubBySuffix maps the part after "github_" to a provider (reserved narrow names).
var githubBySuffix = map[string]Provider{
	"agent": ProviderGitHubAgent,
}

// DetectProvider returns the verification provider for a secret name or note tag.
// Matching: exact secret name, note equal to provider id, or name prefix cloudflare_<suffix>.
func DetectProvider(name, note string) (Provider, bool) {
	name = strings.TrimSpace(name)
	note = strings.TrimSpace(note)
	for _, p := range []Provider{
		ProviderCloudflareAgent,
		ProviderCloudflareBillingRead,
		ProviderGitHubAgent,
		ProviderGitHubPAT,
	} {
		if name == string(p) || note == string(p) {
			return p, true
		}
	}
	if strings.HasPrefix(name, "cloudflare_") {
		suffix := strings.TrimPrefix(name, "cloudflare_")
		if p, ok := cloudflareBySuffix[suffix]; ok {
			return p, true
		}
	}
	if strings.HasPrefix(name, "github_") {
		suffix := strings.TrimPrefix(name, "github_")
		if p, ok := githubBySuffix[suffix]; ok {
			return p, true
		}
	}
	return "", false
}
