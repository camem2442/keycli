# K10 provider lifecycle effects — internal source candidate

Status: **SOURCE_TESTED / PROVIDER_LIVE_NOT_VERIFIED / NOT_PUBLIC_CLI**. This adapter set is designed to be called ONLY from an independently authenticated host, under `rotation.Prepare` / `rotation.Finalize`, an existing durable `journal.Store.Dispatch` and workspace-infra's cross-host Goal Writer Claim. A `HostAuthorize` callback provided by untrusted code **is not an authorization**.

## Narrow effects (one attempt, no retry)

| Provider | Prepare/new generation | Retire exact old generation | Meaning of success |
|---|---|---|---|
| GitHub | Reuse K09-02's **existing** encrypted Actions Secret REST `PutSecretFrom` for a distinct `NAME_GEN_N`. GitHub does not support automatic minting of personal PATs here. | `DELETE /repos/{owner}/{repo}/actions/secrets/{name}`, only old `_GEN_N` and fixed owner/repository ID from a fresh `provideridentity.GitHubRepositorySecret` GET. | 204 *acknowledges deletion request*, not revocation of the original external service credential. There is no compare-and-delete, so cross-host single Writer is mandatory. |
| Cloudflare | `POST /accounts/{id}/tokens`, **account-owned** token, one host-pinned permission group and an exact account resource, token value delivered **once in memory** to a trusted `SecretSink`. | `DELETE /accounts/{id}/tokens/{token_id}`, only the prior verified account-token ID. | Provider acknowledgment only; `Roll` is not used. Issuer/revoker are separate privileged operators. A sink failure after issuance is `UNKNOWN_EFFECT`, not safe to retry. |
| Oracle | OCI Management `PUT /20180608/secrets/{secretId}` with base64 content and `stage=PENDING`, protected by host-pinned `If-Match`. | `POST /20180608/secrets/{secretId}/version/{N}/actions/scheduleDeletion`, only a `DEPRECATED` old version verified by the existing provider identity reader, 1–30 day permanent deletion date. | Pending version/DELETE scheduling acknowledgment only. OCI can make an old version inaccessible **immediately upon scheduling**: that scheduling is never used as a rollback grace period. |

All mutations require an exact trusted-host `HostAuthorize` callback; the caller must bind provider, action, exact destination, grant expiry, write operation ID and verified issuer/revoker principal. The new package alone does not prove that callback's producer is trusted. It does not start runners, alter Cloudflare traffic, refresh IAM policy or issue WIF tokens. Secret bytes must not appear in argv, logs, issue bodies, journal receipts or test fixtures with real values. Provider-response errors are redacted to status/operation class.

The GitHub helper is **only the old generation Secret deletion effect**; encrypted new Secret delivery already exists in KeyCLI #47. GitHub App installation tokens and GITHUB_TOKEN are short-lived and are **not** rotated as long-lived static keys.

Cloudflare creates account tokens via a separately privileged issuer with `Account API Tokens Write`, and revokes via another owner-controlled credential. The permission group ID must be pinned from an independently verified group inventory and must never be the token-management Write group for runtime tokens. A token returned once by the create API is passed to `SecretSink`; an in-memory Go JSON string may remain until GC, so this is not a secure erase claim.

OCI signing occurs **after** method/path/body/`If-Match` is fixed. Oracle's current management API requires a signer correctly scoped to the tenancy, Region, Vault Secret and ETag. An OCI version created as Pending is not automatically a verified new Current version.

All API calls are one-shot, use exact HTTPS origins, disable proxies/redirects and HTTP/2 header debugging. Timeouts, a non-2xx response, or local receipt-storage ambiguity remain unknown-effect and **must not automatically retry**. The actual `rotation.Finalize` host must recheck every consumer, owner grant, old physical ID, current provider GET and original journal intent immediately before effect.

## Verification and operating blockers

`go test -race -count=1 ./internal/rotation/providerlifecycle` is a synthetic transport test only. Real Cloudflare token permissions, GitHub secret selection, OCI Vault version ETag and exact generation are **NOT VERIFIED**. There are no platform keys, mutations, releases or operating grants in this PR. Live issuer/revoker pilots require separate owner approval for one disposable resource and strict rollback/reconciliation evidence.

Official: https://docs.github.com/en/rest/actions/secrets · https://developers.cloudflare.com/api/resources/accounts/subresources/tokens/ · https://docs.oracle.com/en-us/iaas/Content/secret-management/Tasks/create-secret-version.htm · https://docs.oracle.com/en-us/iaas/Content/secret-management/Tasks/delete-secret-version.htm
