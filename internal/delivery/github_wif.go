package delivery

import (
	"errors"

	"github.com/camem2442/keycli/internal/identity/githubwif"
)

const githubOIDCIssuer = githubwif.Issuer

var ErrUnsupportedClaimProbe = errors.New("offline claim probe requires a GitHub Actions WIF profile")

// GitHubClaimProbe describes only a comparison against a caller-provided
// fixture. The zero-valued authority fields are deliberately explicit in JSON.
type GitHubClaimProbe struct {
	Version             int    `json:"v"`
	Profile             string `json:"profile"`
	Provider            string `json:"provider"`
	Mode                string `json:"mode"`
	ClaimsMatched       bool   `json:"claims_matched"`
	Authentication      string `json:"authentication"`
	Authoritative       bool   `json:"authoritative"`
	AcceptanceClaim     bool   `json:"acceptance_claim"`
	AuthorizesExecution bool   `json:"authorizes_execution"`
}

// ProbeGitHubClaims compares a synthetic JSON projection to the selected
// profile. It never accepts or verifies a token.
func ProbeGitHubClaims(profiles Profiles, name string, data []byte) (GitHubClaimProbe, error) {
	p, ok := profiles.entries[name]
	if !ok {
		return GitHubClaimProbe{}, ErrUnknownProfile
	}
	if p.Provider != githubWIF {
		return GitHubClaimProbe{}, ErrUnsupportedClaimProbe
	}

	contract := githubwif.Contract{
		Audience: p.Audience, Repository: p.Repository, Ref: p.Ref,
		WorkflowRef: p.WorkflowRef, Environment: p.Environment,
	}
	if err := contract.CompareSyntheticClaims(data); err != nil {
		return GitHubClaimProbe{}, err
	}
	return GitHubClaimProbe{
		Version: 1, Profile: name, Provider: githubWIF, Mode: "offline-synthetic",
		ClaimsMatched: true, Authentication: "NOT VERIFIED",
	}, nil
}
