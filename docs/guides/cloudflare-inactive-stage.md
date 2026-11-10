---
status: NOT VERIFIED
date: 2026-11-10
owner: keycli
scope: source-only; no provider mutation
---

# K09-04B — Cloudflare version-only journal / observation

**Draft implementation stacked on [K09-04A #48](https://github.com/camem2442/keycli/pull/48), whose base is K09-01~03 [#47](https://github.com/camem2442/keycli/pull/47).** This is a trusted-host Go API. No `kc delivery stage` command, automatic promotion, real token exchange or operating release has been enabled.

## Source boundaries

- `internal/target/cloudflare/version_stage.go` adds an optional operation `--tag kc-<operation-id>` to the existing **`wrangler versions secret put` only** command.
- `version_observe.go` reads Cloudflare **GET-only** deployments, versions and version details using exact account ID/Worker and a scoped credential; the first deployment is the currently serving deployment under the official API list contract. The observer does not read Secret values.
- `internal/delivery/cloudflaredelivery/run.go` requires a separately authenticated host `Authorize` callback, plan digest, immutable account/worker/env, active-deployment digest and original KeyCLI policy. It calls the existing `journal.Store.Dispatch` before invoking a single command. It then checks the active deployment ID/traffic weights are unchanged and exactly one newly listed version has the expected operation tag and named secret binding. Provider success is recorded only as `ACKNOWLEDGED`, **never proof of the secret value**.
- `reconcile.go` uses existing `journal.Store.Reconcile`, GET-only provider reads and operation tag to project `OBSERVED_APPLIED` for a previously ambiguous stage. Missing, stale or ambiguous provider evidence leaves `UNKNOWN_EFFECT`; **no automatic reissue**.

## Non-negotiable live pilot gates

1. Protect account/Worker/Environment name resolution: Wrangler `--name` plus `--env` can resolve a different concrete script name, so the trusted host must independently bind the **actual resolved script identity** before any real run. The current adapter has not proved this on real Wrangler; **NO LIVE CALL UNTIL VERIFIED**.
2. `journal.Store` locks only cooperating processes with the **same private directory**. It does not fence remote Mac/OCI/Cursor hosts. The existing workspace-infra Goal/Writer Claim must own cross-host admission; a callback that returns a Grant without checking genuine authority is not sufficient.
3. Provider GETs must be on the same scoped account/script as the target of Wrangler. If the provider response schema or binding list cannot be parsed, remain `NOT VERIFIED`/`UNKNOWN_EFFECT`.
4. Published status can never be upgraded from `ACKNOWLEDGED` to operating adoption by a self-reported digest, a token that looks valid or CLI exit 0. The host must separately validate the original provider response and the actual consuming Worker.
5. A Version URL is not isolated from production KV/R2/D1/DO. The first live pilot uses a throwaway Worker and independent bindings only. Existing Development cutover [#230](https://github.com/camem2442/workspace-infra/pull/230) and MockTimer Workers are out of scope.
6. The original `kc push cloudflare` still uses `wrangler secret put` (immediate deployment) and must not be used for production secret rotation.
7. Pin Wrangler version and exact verified executable hash. No global `npx wrangler@latest`, production mutation, token rollback/rotation or new paid infra is authorized by this PR.

## Tests / acceptance

Synthetic HTTP observer, inactive Wrangler argument/tag and journal-callback tests cover expected normal effect and denial/unknown outcomes. The isolated Linux amd64 / Go 1.23.2 test with compile-time local shims for the existing `delivery` and `journal` interfaces passed; **full KeyCLI Go 1.22 module CI and real host integration remain NOT VERIFIED**. Merge requires exact-HEAD source + Go 1.22 regression and independent review of Cloudflare script-name/env binding.

Official docs: https://developers.cloudflare.com/workers/wrangler/commands/workers/ and https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/deployments/methods/list/ .
