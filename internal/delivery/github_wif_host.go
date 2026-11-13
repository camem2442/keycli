package delivery

import (
	"errors"
	"os"

	"github.com/camem2442/keycli/internal/identity/githubwif"
)

// GitHubWIFRegistration is supplied by the trusted host for an admitted job.
// The profile snapshot and registration must come from host configuration.
type GitHubWIFRegistration struct {
	Profile  string
	Audience string
	Binding  GitHubWIFBinding
}

// OpenRegisteredGitHubWIF opens the direct WIF route for an admitted GitHub
// Actions job. It checks the profile and audience before reading GitHub's
// job credentials. Exchange.Get performs the first network operation.
func OpenRegisteredGitHubWIF(profiles Profiles, registration GitHubWIFRegistration) (*githubwif.Exchange, error) {
	return openRegisteredGitHubWIF(profiles, registration, os.Getenv)
}

func openRegisteredGitHubWIF(profiles Profiles, registration GitHubWIFRegistration, getenv func(string) string) (*githubwif.Exchange, error) {
	contract, err := githubWIFContract(profiles, registration.Profile, registration.Binding)
	if err != nil {
		return nil, err
	}
	if contract.Audience != registration.Audience {
		return nil, errors.New("GitHub WIF mapping does not match profile audience")
	}
	if getenv("GITHUB_ACTIONS") != "true" {
		return nil, errors.New("GitHub Actions job environment is required")
	}
	requestURL := getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	requestToken := getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
	if requestURL == "" || requestToken == "" {
		return nil, errors.New("GitHub Actions job OIDC credentials are unavailable")
	}
	return githubwif.NewExchange(contract, registration.Binding.Mapping, requestURL, requestToken, nil)
}
