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

## 1a. Mac mac-gui diagnostic observed 2026-11-07 (not yet accepted)

An owner-supplied `kc doctor --json` showed `store.available=true`,
`kc_age=file`, `keychain=unavailable`, two executable paths
(`~/bin/kc`, `~/.local/bin/kc`), and Git credential-helper pinned to the
latter. The `kc ls --json` metadata showed BOTH
`cloudflare_billing_read` and `github_agent` present in the file store, but
both had `aliases: []`; policy check printed `no policy configured (all
reads allowed)`. An installed build `v0.7.2-4-g03d61c6` predates merged
main `400ceec`. These are actual owner-reported observations, not a
Mac native test performed by CI.

**Cause of the two reported `Error: exit status` lines:** the original
`kc exec` uses the canonical upper-case names
`CLOUDFLARE_BILLING_READ` and `GITHUB_AGENT`. Without aliases it does
**not** inject `CF_BILLING_READ_TOKEN` or `GH_AGENT_TOKEN` as expected by
the shell probes. A failing child `test -n` therefore need not mean the
token was missing. `kc exec --require` success and real provider authority
are different facts.

Before making any config edits, check the original variable names without
printing contents (only owner/host local run, **not** GitHub CI):

```sh
type -a kc
"$HOME/bin/kc" --version
"$HOME/.local/bin/kc" --version
KC_AGENT=codex kc exec --require --only cloudflare_billing_read -- \
  sh -c 'test -n "${CLOUDFLARE_BILLING_READ:-}" && printf "KC_BILLING_PRESENT\\n"'
KC_AGENT=codex kc exec --require --only github_agent -- \
  sh -c 'test -n "${GITHUB_AGENT:-}" && printf "KC_GITHUB_PRESENT\\n"'
```

The two `PRESENT` results only indicate local named environment injection;
the tests do not exercise any provider endpoint or assert permissions. Because
no policy is configured yet, **do not treat them as allowlist enforcement**.
Use a backed-up, mode-`0600` copy of the existing config and merge each
`[alias]` entry within the existing section (avoid duplicate TOML tables).
Enable `middleware=["audit","policy"]` outside all TOML sections and
include a `[policy.agent.default]` empty allowlist plus explicit owner-approved
agent scopes. Do not grant the all-accounts `cloudflare_agent` key to a
general development agent. Afterwards verify the actual installed binaries,
`kc doctor`, alias names, policy allow AND deny, and `kc exec` with only
named metadata output. The git helper may still run a different older binary
until its explicit path is separately inspected.

The Keychain unavailability in mac-gui does **not** by itself mean the
available encrypted file store is locked or inaccessible. Do not export the
store, print identity/secret values, copy a full `kc ls` output into public
CI artifacts, or assume the host test authenticates an issuer.

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
