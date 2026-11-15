package delivery

import (
	"errors"

	"github.com/camem2442/keycli/internal/identity/githubwif"
)

type GitHubWIFBinding struct {
	ProviderAlias, ServiceAccountAlias string
	Mapping                            githubwif.Mapping
}

// OpenGitHubExchange binds the selected local WIF profile to the immutable
// mapping and job credentials supplied by an admitted host. Construction does
// not contact GitHub or OpenAI.
func OpenGitHubExchange(profiles Profiles, name string, binding GitHubWIFBinding, requestURL, requestToken string, sdk githubwif.SDKExchange) (*githubwif.Exchange, error) {
	contract, err := githubWIFContract(profiles, name, binding)
	if err != nil {
		return nil, err
	}
	return githubwif.NewExchange(contract, binding.Mapping, requestURL, requestToken, sdk)
}

func githubWIFContract(profiles Profiles, name string, binding GitHubWIFBinding) (githubwif.Contract, error) {
	p, ok := profiles.entries[name]
	if !ok {
		return githubwif.Contract{}, ErrUnknownProfile
	}
	if p.Provider != githubWIF {
		return githubwif.Contract{}, errors.New("GitHub Actions WIF profile required")
	}
	if binding.ProviderAlias != p.WIFProviderAlias || binding.ServiceAccountAlias != p.ServiceAccountAlias {
		return githubwif.Contract{}, errors.New("GitHub WIF mapping does not match profile aliases")
	}
	return githubwif.Contract{
		Audience: p.Audience, Repository: p.Repository, Ref: p.Ref,
		WorkflowRef: p.WorkflowRef, Environment: p.Environment,
	}, nil
}
