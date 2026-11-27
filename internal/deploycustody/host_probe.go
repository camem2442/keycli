package deploycustody

import (
	"runtime"
	"sort"
)

// HostProbeResult contains non-sensitive presence metadata. A local process
// cannot prove absence of tokens in KeyCLI, Keychain or another host's process.
type HostProbeResult struct {
	Schema                   string   `json:"schema"`
	Platform                 string   `json:"platform"`
	CredentialEnvNames       []string `json:"credential_env_names"`
	LocalSecretStores        string   `json:"local_secret_stores"`
	OtherProcessEnvironments string   `json:"other_process_environments"`
	ClaimStore               string   `json:"claim_store"`
	Status                   string   `json:"status"`
	AuthorizesCredentialRead bool     `json:"authorizes_credential_read"`
	AuthorizesDeploy         bool     `json:"authorizes_deploy"`
	AuthorizesRecovery       bool     `json:"authorizes_recovery"`
}

var credentialEnvNames = []string{
	"CF_API_TOKEN", "CLOUDFLARE_API_TOKEN", "CLOUDFLARE_API_KEY",
	"CF_API_KEY", "CLOUDFLARE_API_EMAIL", "CF_EMAIL",
}

// ProbeEnvironment never reads or displays credential values. Environment
// absence is not proof that this host or another host lacks a deploy key.
func ProbeEnvironment(lookup func(string) (string, bool)) HostProbeResult {
	r := HostProbeResult{
		Schema:                   "keycli/deploy-host-probe/v1",
		Platform:                 runtime.GOOS + "/" + runtime.GOARCH,
		LocalSecretStores:        "NOT_INSPECTED",
		OtherProcessEnvironments: "NOT_INSPECTED",
		ClaimStore:               "NOT_AUTHENTICATED",
		Status:                   "HOLD_HOST_CUSTODY_UNVERIFIED",
	}
	if lookup == nil {
		return r
	}
	for _, key := range credentialEnvNames {
		_, present := lookup(key)
		if present {
			r.CredentialEnvNames = append(r.CredentialEnvNames, key)
		}
	}
	sort.Strings(r.CredentialEnvNames)
	if len(r.CredentialEnvNames) > 0 {
		r.Status = "HOLD_ENV_CREDENTIAL_SURFACE_PRESENT"
	}
	return r
}
