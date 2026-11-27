# MockTimer Cloudflare custody: authenticated read-only snapshot · 2026-11-20

## Source provenance and observed limits

A connected Cloudflare account lookup located the MockTimer account. A read-only `GET /accounts/{account_id}/tokens` returned **0 Account API Token entries** for that account. This is **NOT** a claim that user API tokens, global keys, OAuth sessions, Mac Keychain, KeyCLI encrypted stores or process environments are empty.

A read-only `GET /user/tokens` returned HTTP **403 / 9109** (no API Tokens Read/Write permission on this connection). No attempt was made to widen account permissions or create/revoke/rotate a token.

The Worker deployment API returned `mocktimer-members-production` deployment `f77685d4-4ab6-4bcc-92b5-534ef362b45f` at **2026-11-20T10:45:39.402634Z**, 100% version `94e95bde-e40f-4bed-a2ea-4f503439f606`. This is newer than the last previously recorded local deploy ledger row at 2026-11-20T08:39Z. The actual live source revision and any independent host credentials remain **UNVERIFIED**. Source Git branches must not be presumed to equal this live version.

## Host-only safe capability probe

`go run ./cmd/kc-deploy-hostcheck --json` provides a bounded **current process** inspection. It emits only the names of known Cloudflare credential environment variables that are present. Values are never printed, hashed, copied or used. `local_secret_stores`, `other_process_environments` and `claim_store` intentionally remain NOT_INSPECTED/NOT_AUTHENTICATED. A no-env result is still HOLD.

Use the same exact source program under the existing independently admitted Mac, Flip3 and VM host owners; the runtime platform field is informative, not authenticated host identity. Do not run from an untrusted CI principal that has a real provider secret mounted, and never add the secret as a GitHub Actions environment value.

## Before operating any exclusive deploy owner

1. Select the one owner host/OS principal and bound KeyCLI Effect Gateway/ClaimStore generation; inventory all live Writer and provider credential surfaces and verify non-owner denial without disclosing values.
2. Keep the owner user's interactive recovery path, then stage narrowly scoped product Worker deployment credentials under the separate trusted OS principal/KeyCLI gateway. Do not simply set `KC_AGENT=human` for agents.
3. Validate the actual token policy for only the expected Cloudflare account/Worker in a trusted owner process. Prove that the two independent clone/other host requesters cannot use the same deployment credential.
4. Preserve a reversible previous deployed Worker version, provider-readback and `UNKNOWN_EFFECT` recovery procedure; never auto-release on TTL alone. Do not revert a live deployed version merely to make a Git branch look current.

This source review contains no account IDs, token IDs, token bytes, email addresses or host user data and performs **zero permission changes**. Privileged Credential read/rotation/revocation must remain behind the original KeyCLI Authority/Effect Gateway; a source PR cannot supply the missing host attestations.

## Provider authorization shape (official 2026-09 policy)

Cloudflare now supports API tokens with **Individual Workers → Editor** for existing Worker deployments, instead of broad Workers product Editor. Prefer exactly the existing `mocktimer-members-production` Worker and no Worker creation/deletion permission. This differs from the legacy `Workers Scripts Write` permission and must be verified against the actual token policy before adoption. Deploying a bound Worker does not by itself need D1 database query/write permission; those effects are separate. Custom Domain operations may require broader/zone-scoped rights, and are not silently authorized.

Official documentation: https://developers.cloudflare.com/workers/authorization/workers/ and https://developers.cloudflare.com/workers/authorization/ .
