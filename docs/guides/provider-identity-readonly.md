# K10-05 — read-only Provider Identity observers (proposal)

Status: **SOURCE TESTED / OPERATING NOT VERIFIED**, 2026-11-10.

## Source and bounds

- Base: `camem2442/keycli` [PR #56](https://github.com/camem2442/keycli/pull/56) at `54b087e304dd003c6371bce353f8e1ccdc7ebdd3`.
- New package: `internal/rotation/provideridentity` contains concrete GET-only GitHub, Cloudflare and OCI Vault metadata readers. Each `Verify(ctx, plan, handle)` matches the already-implemented `rotation.VerifyOldCredential` callback consumed by `Finalize`.
- Existing `internal/rotation/revocation_identity.go` and `finalize.go` remain **unchanged**. No revocation, issuance, deployment or token recovery in this package.
- A **separately trusted host** supplies authenticated HTTP request callbacks, a pinned `PlanTarget`, exact resource IDs and issuer-owned timestamps/policy hashes, a writer fence and the Finalize revocation grant. Public JSON, a `ProofDigest` or a value-returning model invocation cannot establish trust.

## Real provider read-only endpoints

| Provider | GET only | Exact refusal criteria | Non-goals |
|---|---|---|---|
| GitHub repository Actions Secret | `/repos/{owner}/{repo}` then `/repos/{owner}/{repo}/actions/secrets/{secret_name}` | repository ID, owner ID, exact generation-specific name, independently pinned `created_at`/`updated_at` mismatch | Returns metadata only, **never secret value**; no CAS delete. Only repository scope in this first adapter; environment/org separate future scope |
| Cloudflare account-owned API Token | `/client/v4/accounts/{account_id}` then `/client/v4/accounts/{account_id}/tokens/{token_id}` | account/token ID, token name, `issued_on`, status, active expiry, *exact pinned policy digest* and account-scoped policy mismatch | Worker Runtime Secret **not** account token; Account API Tokens Read privilege required, do not widen on 403 |
| Oracle Vault Secret Version | `https://vaults.{region}.oci.oraclecloud.com/20180608/secrets/{secretId}` and `/version/{versionNumber}` | signer-tenancy assertion, secret/compartment/vault ID, version number, `DEPRECATED` stage, no scheduled deletion | **Management metadata only**, no `GetSecretBundle`, no secret content, no deletion schedule |

The Go HTTP reader uses pinned HTTPS origins, GET, no redirects, no ambient proxy, bounded JSON, no raw response logging, and host-only authorization callbacks. Tests substitute HTTP transport but do not represent real provider token validation. Oracle's `Authenticate` callback must implement actual OCI request signing; `VerifySignerTenancy` must independently verify signer tenancy before any GET. If host evidence is missing, fail closed.

## Security implications

- Secret values never enter the observer's output. GitHub's GET is **not** a value proof. A secret may be overwritten between GET and later DELETE: a host-wide writer fence, an independent pre-delete observation and last-moment owner grant remain mandatory, and GitHub has no DELETE CAS.
- The Cloudflare Token Details payload can contain other fields: the typed decoder ignores values, never logs the raw payload, and requires exact account-owned token scope and a pinned policy digest. On missing `Account API Tokens Read`, return HOLD rather than using a more privileged or user-owned token by fallback.
- OCI Version stage `CURRENT`, `PENDING`, `LATEST`, `PREVIOUS`, unknown or `timeOfDeletion != null` are **not** eligible for obsolete-version deletion. Oracle docs allow deletion scheduling only for `DEPRECATED` versions. Actual scheduling can affect access before permanent deletion; require verified rollback window.
- `OldIdentityDigest` is a deterministic *equality binding* over metadata, not host attestation. An untrusted model could fake this digest and an API transport. Only a separately trusted host grants `Finalize` authority.

## Synthetic tests and remaining gates

- `GOTOOLCHAIN=local CGO_ENABLED=0 go test -count=1 ./...` and `CGO_ENABLED=1 go test -race -count=1 ./...` in an isolated **Go 1.23.2** module, with upstream KeyCLI rotation production sources at PR #56 and a **local journal test shim**. Result: PASS for this bounded scope; source SHA of `revocation_identity.go` and `finalize.go` is identical to PR #56.
- Tests include successful three-provider GET checks; wrong repo, secret timestamp drift, 403, wrong Cloudflare ID, disabled/wildcard policy, wrong OCI secret, current stage, deletion scheduled, signer-tenancy missing, no redirects/unexpected host; a `Finalize` integration fixture proves drift prevents the revoker callback. This is neither an actual issuing/revoking API integration nor a production bearer / OCI signer test.
- Real KeyCLI Go 1.22.12 full module and exact HEAD Linux CI **NOT VERIFIED** for this new PR. Reuse existing [#57 CI](https://github.com/camem2442/keycli/actions/runs/42774640356) for unchanged upstream blobs, then exact new HEAD verification separately when authorised and cost-gated.
- Operating provider identities and credential revocation not verified. No client secret/personal key supplied, no real provider requests, no secret writes or deletes.

## Official docs

- https://docs.github.com/en/rest/actions/secrets
- https://developers.cloudflare.com/api/resources/accounts/subresources/tokens/methods/get/
- https://docs.oracle.com/en-us/iaas/api/
- https://docs.oracle.com/en-us/iaas/Content/secret-management/Tasks/list-secret-version.htm
- https://docs.oracle.com/en-us/iaas/Content/Identity/Reference/keypolicyreference.htm
