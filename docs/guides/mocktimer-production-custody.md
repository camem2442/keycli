# MockTimer production credential custody — KeyCLI source integration

2026-11-20 · Q2 · **source-only**. This contract reuses the existing KeyCLI [Effect Gateway](../contracts/effect-gateway-v1.md), host Claim/Grant and durable delivery journal; it does not introduce a second token issuer or generic Wrangler executor.

## Shared target / identity
- Product repository: `camem2442/mock-exam-timer`
- Target key: `cloudflare:mocktimer-members-production`
- Source: immutable 40-hex SHA on `release/<YYYYMMDD>` (not `main`, not an arbitrary working branch)
- Only one owner-host credential surface; all other Mac / Flip3 / VM / cloud agent surfaces must be **observed** to lack Production write capability before enabling anything.
- Read-only Billing Read aliases and general developer `cloudflare_agent` keys are **not** Production deployment credentials. Never grant them blanket Worker deployment.
- Secret VALUE, derived digest, raw issuer header, account token, private `~/.kc` content, or environment dumps must never appear in repository files or CI.

## Existing owners, not duplicate implementations

KeyCLI `internal/effectgateway` authenticates a signed host intent, validates exact Goal Claim/Grant, and persists an effect through `internal/delivery/journal`. Its journal preserves UNKNOWN_EFFECT and refuses replay. It is single-host and must not be interpreted as cross-host OS isolation. The MockTimer product PR [#805](https://github.com/camem2442/mock-exam-timer/pull/805) adds a **separate source-only** host slot and exact-source/guard checks; nothing here registers an active production KeyCLI provider route.

`internal/deploycustody.Assess` reports **metadata-only** candidate consistency. `PLAN_ONLY_NEEDS_NATIVE_ATTESTATION` is NEVER permission to read a credential, deploy a Worker, release a lock or reconcile an unknown provider effect. All output permission bits remain false.

## Native adoption sequence — not executed by this PR

1. Select an existing admitted owner host and secure/private (0700) host-side state root; inventory actual running sessions and independent clones. Confirm fresh Host Owner/Goal Claim and a credential scope limited to the product Worker.
2. Configure a **separate** deploy-only KeyCLI alias under a dedicated trusted execution principal, not a same-UID `KC_AGENT=human` policy label. No copy into agent workspaces, shell startup, GitHub Actions, or other hosts.
3. Prove non-owner-deny + owner-allow with synthetic credentials first. Preserve owner interactive recovery and existing KeyCLI rollback. Do not mint, revoke or rotate a real Cloudflare token in this source PR.
4. Obtain current deployment/version/route identity from independent authenticated Cloudflare readback and `/version.json`. Before any production effect, migrate the product's direct Wrangler path behind the authenticated existing Gateway / credential issuer and explicitly pin exact operation target and Grant.
5. For a conflict, missing host coverage, 45-minute local lease expiry, unconfirmed/outstanding upload, or unexpected provider version, keep the product slot CLOSED and journal `UNKNOWN_EFFECT`; only provider-confirmed independent reconciliation may permit later release/rollback. Never auto-break a lock on timestamp/PID.
6. Run two independent clone denial cases, simultaneous source-only contenders, an interrupted fake Wrangler job and replay denial in an isolated host harness. Only then stage an actual host-owned native acceptance.

## Relationship to common workspace sessions

Use [workspace-infra session contract](https://github.com/camem2442/workspace-infra/blob/ba04f6a4acc21d8f449ca8dc915dca8fd6e874a4/docs/contracts/multi-session-roots-v1.md) for worktree entry. `workspace_cli.py session inspect/list` is a LOCAL snapshot, and `session new/reenter` must use an exact digest request handled by the existing Lifecycle. A `.session.json` marker never grants KeyCLI access.

Scope exclusions: no Cloudflare deployment or account mutation, no token extraction, no Mac Keychain writes, no host user creation, no old worktree cleanup, no operating proof or source auto-merge.
