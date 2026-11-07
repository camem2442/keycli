---
status: PARTIAL
owner: keycli
date: 2026-11-07
scope: local and platform-native credential presence; no secret transfer
---

# Scoped token reuse for development agents — source-only adoption gate

This is a local operating recipe, **not** evidence of successful real-token access.
It never authorizes PR merges, Work Key issuance, Cloudflare deployment or token export.

## 1. Names and environment binding

**Merge** these entries into the existing `~/.config/kc/config.toml` without
overwriting other aliases, backend orders or links. Names are examples; confirm
actual canonical names with `kc ls --json` (metadata only).

```toml
middleware = ["audit", "policy"]

[audit]
enabled = true
required = true
path = "~/.kc/audit.log"

[alias]
github = ["gh", "GITHUB_TOKEN", "GH_TOKEN"]
github_agent = ["GH_AGENT_TOKEN"]
cloudflare_billing_read = ["CF_BILLING_READ_TOKEN"]

[policy.agent.codex]
allow = ["github_agent", "cloudflare_billing_read"]

[policy.agent.claude]
allow = ["github_agent"]

[policy.agent.cursor]
allow = ["github_agent"]

[policy.agent.antigravity]
allow = []

[policy.agent.default]
allow = []
```

`github` (legacy Git credential helper) and `github_agent` (dedicated agent
token) should remain distinct if their access scopes differ. `GH_TOKEN`
and `GITHUB_TOKEN` are recognized by GitHub CLI; do **not** silently alias
a high-privilege token to all environments.

## 2. Metadata-only local checks, with no raw values

```bash
kc --version
kc doctor --json
kc ls --json
KC_AGENT=codex kc policy check cloudflare_billing_read

KC_AGENT=codex kc exec --require --only cloudflare_billing_read -- \
  sh -c 'test -n "${CF_BILLING_READ_TOKEN:-}" && test -z "${KC_AGENT:-}" && printf "billing env: PRESENT\\n"'

KC_AGENT=codex kc exec --require --only github_agent -- \
  sh -c 'test -n "${GH_AGENT_TOKEN:-}" && test -z "${KC_AGENT:-}" && printf "github env: PRESENT\\n"'
```

The `PRESENT` checks establish only that the owned child received a named
environment value. They **do not** authenticate a provider, token issuer,
repository permission, host or human. Missing names must prevent child launch
(exit 2); policy denial must prevent launch (exit 5). No `kc get`,
`env`, `printenv`, `set -x` or token contents should appear in receipts.

A real *read-only* GitHub identity probe, if authorized by the operator, can be:
```bash
KC_AGENT=codex kc exec --require --only github_agent -- \
  sh -c 'GH_TOKEN="${GH_AGENT_TOKEN:?}" gh api user --jq .login'
```
GitHub identity HTTP 200 is not repository write/merge authority. Test
required repository ruleset/protection permissions independently and never
retry 403 by broadening a token.

## 3. Cloudflare Billing Read / GitHub protected environment

`cloudflare_billing_read` is a *different* credential from
`cloudflare_agent`. Once the corrected and independently reviewed
`workspace-infra` Billing Read source on PR #111 is in main, use either:

- Local explicit `kc exec --only cloudflare_billing_read --require` into
  `tooling/cloudflare_billing_readonly.py --read-only --output <0600 local path>`.
- Main-only GitHub Actions `cloudflare-billing-readonly.yml` manual
  `workflow_dispatch`, whose `account-read` job uses the protected
  `billing-readonly` environment's `CF_BILLING_READ_TOKEN` secret.

Neither path should print raw billing API response bodies, raw tokens, personal
billing profile information, or full headers. A local credential does not
appear automatically in a GitHub Actions secret, and vice versa. Do not add
token forwarding or Cloudflare secret writes as part of this adoption.

Workers plan, billing cycle, spend-policy `404`, and usage require distinct
provider-issued evidence. A subscription list, `billable-usage` 200,
`default_usage_model=standard`, or token verification cannot by itself
prove Workers Free or zero charges.

## 4. Multi-host limitations and explicit gates

- `KC_AGENT` and keycli policy labels are user-controlled accidental-misuse
  guardrails; they are not OS identity, Work Key issuer authentication,
  or authorization for PR merge/deploy. Named `default` fallback is proposed
  by this source-only PR for unregistered agents.
- `kc exec --only` limits secrets **injected by keycli**; it does not erase
  unrelated environment variables already present in the parent process.
  Start each trusted agent with a scoped platform environment / host isolation.
- The metadata-only `kc mcp` default introduced in released v0.7.2 must
  remain the shared-agent default. Never enable `get_secret` as a shared
  tool without a separate trust decision.
- Codex, Claude, Cursor local/Cloud and GitHub Actions each require
  independently observed one-turn allowed/denied/missing tests, exact binary
  SHA and host receipts. No synthetic pilot is a real-token acceptance.
- Neither `kc push cloudflare`, `kc push gh`, `kc export` nor production
  secret rotation is part of this verification. Cloudflare Staging remains HOLD.
- Keep Goal / Work Key / Event issuance in workspace-infra; Cloudflare
  observations remain OWNER_REPORTED until authenticated original issuer evidence.

See [v0.8 multi-agent architecture](../planning/v08-multi-agent-auth-infrastructure.md),
[the keycli integration rehearsal](https://github.com/camem2442/keycli/pull/25)
and [workspace-infra Billing Read](https://github.com/camem2442/workspace-infra/pull/111).
