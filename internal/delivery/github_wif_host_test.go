package delivery

import (
	"fmt"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/identity/githubwif"
)

func TestOpenRegisteredGitHubWIF(t *testing.T) {
	profiles, err := ParseProfiles([]byte(cloudflareProfile + otherProfiles))
	if err != nil {
		t.Fatal(err)
	}
	const fixtureToken = "synthetic-job-request-bearer"
	for _, tc := range []struct {
		name    string
		change  func(*GitHubWIFRegistration)
		unset   string
		noEnv   bool
		wantErr bool
	}{
		{name: "registered direct route"},
		{name: "unknown profile", change: func(r *GitHubWIFRegistration) { r.Profile = "missing" }, noEnv: true, wantErr: true},
		{name: "non WIF profile", change: func(r *GitHubWIFRegistration) { r.Profile = "cloudflare_dev" }, noEnv: true, wantErr: true},
		{name: "wrong audience", change: func(r *GitHubWIFRegistration) { r.Audience = "https://example.invalid" }, noEnv: true, wantErr: true},
		{name: "wrong alias", change: func(r *GitHubWIFRegistration) { r.Binding.ServiceAccountAlias = "other" }, noEnv: true, wantErr: true},
		{name: "missing exact subject", change: func(r *GitHubWIFRegistration) { r.Binding.Mapping.Subject = "" }, wantErr: true},
		{name: "outside Actions", unset: "GITHUB_ACTIONS", wantErr: true},
		{name: "missing OIDC endpoint", unset: "ACTIONS_ID_TOKEN_REQUEST_URL", wantErr: true},
		{name: "missing OIDC request token", unset: "ACTIONS_ID_TOKEN_REQUEST_TOKEN", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{
				"GITHUB_ACTIONS":                 "true",
				"ACTIONS_ID_TOKEN_REQUEST_URL":   "https://pipelines.actions.githubusercontent.com/oidc/token?api-version=7.0",
				"ACTIONS_ID_TOKEN_REQUEST_TOKEN": fixtureToken,
			}
			delete(env, tc.unset)
			r := GitHubWIFRegistration{
				Profile: "github_openai", Audience: "https://api.openai.com/v1",
				Binding: GitHubWIFBinding{
					ProviderAlias: "github-fixture", ServiceAccountAlias: "ci-fixture",
					Mapping: githubwif.Mapping{
						ProviderID: "provider-fixture", ServiceAccountID: "account-fixture",
						RepositoryID: 123, RepositoryOwnerID: 456,
						WorkflowSHA: strings.Repeat("a", 40), Subject: "repo:example/fixture:environment:ci",
					},
				},
			}
			if tc.change != nil {
				tc.change(&r)
			}
			exchange, err := openRegisteredGitHubWIF(profiles, r, func(name string) string {
				if tc.noEnv {
					t.Fatal("read job environment before rejecting registration")
				}
				return env[name]
			})
			if tc.wantErr {
				if exchange != nil || err == nil || strings.Contains(err.Error(), fixtureToken) {
					t.Fatal("expected redacted registration rejection")
				}
				return
			}
			if err != nil || exchange == nil || exchange.Route() != "direct" {
				t.Fatalf("expected registered direct exchange: %v", err)
			}
			if strings.Contains(fmt.Sprintf("%+v", exchange), fixtureToken) {
				t.Fatal("exchange exposed job bearer")
			}
		})
	}
}
