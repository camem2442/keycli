---
status: partial
owner: delivery
---

# GitHub Actions → OpenAI workload identity federation

The trusted GitHub Actions job host opens its admitted delivery profile through
`delivery.OpenRegisteredGitHubWIF`. The host supplies the approved profile
snapshot and a registration with the exact profile name, audience, provider and
service-account aliases, OpenAI provider and service-account IDs, GitHub
repository and owner IDs, workflow SHA, and subject. Keep the profile snapshot
and registration under host control. Workflow inputs and workspace files from
the job must not be able to replace them.

The approved job needs `id-token: write` and the GitHub environment named in
the profile. The adapter reads `ACTIONS_ID_TOKEN_REQUEST_URL` and
`ACTIONS_ID_TOKEN_REQUEST_TOKEN` from that job. Opening the adapter has no
network effect. The trusted caller uses `Exchange.Get(ctx)` to acquire an
opaque credential in memory. Callers that use `Credential.Authorize` must
disable redirects, ambient proxies, and authorization header logging on
their OpenAI API transport.

The exchange asks GitHub for the registered audience, then compares the
returned claims with the profile and registration before sending the JWT to
`https://auth.openai.com/oauth/token`. This includes the repository name and
IDs, workflow reference and SHA, environment, event, ref, audience, subject,
and validity period. OpenAI verifies the signature and applies the
configured mapping. Register the exact subject OpenAI expects, including
any GitHub customization, and require matching issuer, audience, and
repository/workflow identity in the OpenAI provider mapping.

The job endpoint must use HTTPS on a single-label runner subdomain under
`actions.githubusercontent.com`. The adapter preserves the job request URL
and appends the registered audience when absent. It rejects conflicting or
duplicate audience parameters.

The direct transport disables redirects and proxy discovery. It sends a
single token-exchange POST per refresh attempt and reports a redacted
failure if the result is unavailable. Credentials stay in memory and are
usable only within the validated expiry bounds. There is no static API key
fallback.

Official contracts checked 2026-11-12

- [GitHub OpenID Connect reference](https://docs.github.com/en/actions/reference/security/oidc)
- [OpenAI federation for GitHub Actions](https://developers.openai.com/api/docs/guides/workload-identity-federation/github-actions)
- [OpenAI workload identity token exchange](https://developers.openai.com/api/reference/workload-identity-federation)

PR-13 owns live GitHub job enrollment, OpenAI provider and service-account
mapping verification, and a limited OpenAI API request. This contract
describes the host adapter and its synthetic verification scope.
