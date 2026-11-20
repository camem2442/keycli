---
status: source-candidate-no-provider-effect
owner: keycli
date: 2026-11-21
---

# Q1 — portfolio Gateway exact default-environment secret staging

## Original source and provider observations

- [Context Platform #91](https://github.com/camem2442/context-platform/pull/91) is merged; it implements a read-only host fan-in. [#92](https://github.com/camem2442/context-platform/pull/92) is the independent source PR for two new per-repository Dev Gateway Worker configs. It is **not** permission to deploy.
- On 2026-11-21 the connected Cloudflare Development Infrastructure account listed the existing `context-platform-gateway-p1` Worker, whose source remains `camem2442/obsidian-documents`, and **neither** `context-platform-portfolio-infra-dev` nor `context-platform-portfolio-context-dev`. No Worker/token/policy was changed by this review.
- Cloudflare's official [Secrets docs](https://developers.cloudflare.com/workers/configuration/secrets/) and [Worker Wrangler commands](https://developers.cloudflare.com/workers/wrangler/commands/workers/) distinguish `wrangler secret put` (immediate version deployment) from `wrangler versions secret put` (inactive version only).
- KeyCLI's existing `StagePinnedVersionSecret` previously required a nonempty `Environment` and always appended `--env`. #92's new configs explicitly target **default Worker environments**, not `[env.staging]`.

## Tiny source correction

`internal/target/cloudflare/version_stage.go` now accepts `VersionStageRequest.UseDefaultEnvironment=true` **only** if:
1. `Environment` is exactly empty, and the targeted TOML config is a bounded regular file;
2. the config's `name` matches the exact requested Worker and `account_id` matches the exact requested Cloudflare account;
3. the config does not define any nonempty named `[env.*]` settings;
4. all existing process pin, caller-supplied `beforeStart`, explicit account-scoped `CLOUDFLARE_API_TOKEN`, environment allowlist, secret stdin, timeout and unknown-effect stop conditions remain unchanged.

The opt-in command omits `--env`; all existing named-environment callers keep their prior arguments unchanged. This is a **generic KeyCLI adapter**, not hardcoded portfolio authorization. The source-only check is not a trusted user Mac authorization or evidence that a secret value exists.

### Actual operating sequence — not executed here

- Source must first pass Go 1.22+ exact-head verification and native caller/KeyCLI owner admission; preserve the original user secret storage and current Mac installed version.
- Each target requires its own private-repository GitHub **contents:read** token (limited to exactly one repository), distinct Gateway `CALLERS_JSON` bearer and distinct `CURSOR_SECRET`. The existing connected ChatGPT GitHub identity cannot be exported as a Worker credential.
- New Workers **do not currently exist**: do not pretend that an inactive version stage created them or pre-provision `kc push cloudflare` (its legacy `wrangler secret put` path changes traffic). The owning Cloudflare setup must first provision exactly one non-public Worker in an admitted, atomically-secret-bound operation, observe its version, policy and bearer deny. The `[secrets].required` contract on #92 prevents an ordinary deployment without configured values. Authorization, costs and secret provenance belong to the exact-target owner.
- Only **after** that Worker exists and has an independently observed pinned revision may the trusted host use the opt-in inactive version command with the profile's exact config path, Worker, account and approved target/Grant/Writer. A successful process exit is `ACKNOWLEDGED_UNVERIFIED`; require Cloudflare version and deployment GET evidence and separate traffic promotion consent. Any ambiguous provider result is `UNKNOWN_EFFECT`, never auto-retried.
- The two `/v1/query` endpoints can be used by #91 only when each endpoint/bearer/source corpus is separately authenticated, Git SHA/blob/URL are exact and the original Study Gateway remains unchanged.

## Verification boundary

Focused Go unit tests cover opt-in default command construction, named-env backward compatibility, config Worker/account mismatch, fake `[env]` denial, absent opt-in, pinned executable and mandatory pre-dispatch callback. No real Wrangler, Mac Keychain, Github credential issuance or Cloudflare effects. This repository's `.github/workflows/test.yml` does **not** admit normal PR execution on its public/self-hosted runners; owner independent source verification is needed for technical Ready/merge eligibility. Do not substitute a hosted paid runner or claim success from static tests not executed.
