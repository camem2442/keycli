# Cloudflare Free-only signed disposable pilot gate (source candidate)

Status: **SOURCE_TESTED / CLOUD_PROVIDER_NOT_ATTESTED / LIVE_HOLD**. This document is a planned KeyCLI internal control, not an operating grant or a proof that an account is on the Workers Free plan.

## Why an owner-signed record is required

Cloudflare Development Infrastructure account `b398955bcfe917a4af61d179af967b6a` returned six existing Worker scripts and `default_usage_model=standard` on 2026-11-10. `GET /accounts/{id}/subscriptions` and `/billing/usage` returned **403**; `GET /accounts/{id}/entitlements` succeeded with an empty list. **None of those observations establishes a Free subscription or $0 spending.** Do not infer plan from account names, Worker count, `standard` usage model, or undocumented response fields.

The `internal/delivery/freegate` package is a strict **read-only preflight**. It requires:

- A pinned Ed25519 public key held **outside** the submitted evidence, with a domain-separated owner-signed account-specific dashboard assertion of Workers Free, captured within 24 hours. The owner must verify the actual plan in the authoritative billing dashboard and take responsibility for the claim. A model-supplied boolean or arbitrary signature key does not qualify.
- A live read-only provider Worker list, complete and sorted, matching the signed list, and matching the **host-pinned account ID**. An empty entitlements list is not proof of Free.
- Only a synthetic Worker named `kc-v09-stage-pilot-*`, no existing Worker reused for bootstrap, account count <100, no routes/custom domain/cron, no `workers_dev`, no R2/KV/D1/DO bindings, extra USD budget exactly zero, and bounded planned calls (1 bootstrap, 1 inactive Stage, 1 cleanup, <=32 GET).
- Each of `bootstrap`, `stage`, and `cleanup` uses a **fresh signed operation-scoped assertion** and a current provider list. A signature does not authorize any write; response always sets `authoritative=false` and `authorizes_execution=false`.
- Any missing proof, expired observation, wrong account, increased Worker count, mismatched signature, denied remote read, or unknown cost stays `BLOCKED`. A separate workspace-infra Goal/Claim/Writer grant, actual permission scope and a provider cost gate must admit a subsequent effect. The Gate does not deploy any Worker.

Official public Free envelope: 100 Workers/account, 100,000 requests/day; these are *limits*, not evidence that this account is Free or has room in its current request budget. https://developers.cloudflare.com/workers/platform/limits/

## Source tests

`go test -race -count=1 ./internal/delivery/freegate` uses only locally generated Ed25519 synthetic keys and a fake read-only Worker inventory. It tests a correct owner-scoped record, tampering, wrong account, paid binding, unknown plan, missing record, expired signature, count at capacity, missing target and stale live inventory.

**Not implemented:** secure dashboard capture and attestation issuance by the owner, true authenticated production host registration, signed inventory provider collector, real permission/plan visibility, disposable Worker staging and cleanup. Those are separate operating approvals. Never create a Worker to "discover" whether the account is Free.
