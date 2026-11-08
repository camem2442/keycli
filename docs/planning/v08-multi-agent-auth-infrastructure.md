---
status: PARTIAL
owner: keycli
source_head_at_planning: bb19ba0d489dd84b3f8d3b7169c73150d51d262a
date: 2026-11-07
---

# v0.8.0 proposal — common authentication for development agents (Q3)

**Product boundary:** keycli is a local resolution/injection/transfer client, **not** a central secret vault, a Git write authority, an OS isolation boundary, or a background credential synchronizer. This roadmap is an adoption proposal, not a claim of completed cross-provider E2E.

## 1. Verified source vs unverified deployment

- v0.7.1 tagged release and Linux CI: **COMPLETE** for their declared scope (2026-11-05). Earlier v0.7.0 operational close is historical and stays closed.
- Native `kc mcp`, TUI and `kc push cloudflare`: source and Linux unit tests present; actual provider/GUI/Worker acceptance **NOT VERIFIED**.
- **v0.7.2 release closure:** published [v0.7.2](https://github.com/camem2442/keycli/releases/tag/v0.7.2) includes merged PR #16 metadata-only MCP default and opt-in secret values. The released Linux binary was downloaded, checksum-verified, reinstalled to an isolated prefix and exercised ([evidence](v072-release-evidence.md)); real Mac/client acceptance remains **NOT VERIFIED**. The earlier v0.7.1 binary still advertises `get_secret` by default.
- Mac TUI/Cursor DB protective gate: source fix [PR #17](https://github.com/camem2442/keycli/pull/17) merged, Linux CI **COMPLETE**. Real Mac native acceptance **NOT VERIFIED** ([matrix](v08-mac-tui-cursor-acceptance.md)).
- Cursor Cloud install/activate, Mac `kc push cursor` real PUT and GitHub `--app actions` paths were recorded in [v0.7 operational evidence](v07-operational-evidence.md), **COMPLETE only for those tested scopes**.
- Cursor alias-through-real-PUT, GitHub other `--app`/`--user` combinations, Cloudflare real Wrangler, Claude/Codex 1-turn `kc` E2E, and live multi-agent policy enforcement: **NOT VERIFIED**. KC-25 (gh 401) stays conditional on reproduction.

## 2. Ownership and data flow

```text
Human-owned local secret origin (macOS Keychain / encrypted file)
       │  lookup by explicit name/scope; never source-wide sync
       ├── kc exec --require --only ... ──> local child process (short-lived env)
       ├── kc git-credential + setup-git ──> Git HTTPS auth (helper, not exec alone)
       └── authorized kc push ──> platform-native secret store
                                    ├─ Cursor Cloud environment
                                    ├─ GitHub Actions/Codespaces (scope-specific)
                                    └─ Cloudflare Worker secret (Wrangler)
                                           │
                                Platform injects runtime env
                                           │
                                    cloud kc exec
                                           │
                                  agent owning command
                                           │ (NO secret values)
                          keycli local audit + verification receipts
                                           │
                           workspace-infra Goal/Delivery owner
                                           │
                         optional Cloudflare metadata only
```

**Non-negotiable boundaries**

1. Never transmit `KC_KEY`, a full local vault, raw DBs, auth tokens, or plain secret values through GitHub commits, review comments, Goal receipts, Cloudflare R2/KV/DO, logs, or MCP metadata calls.
2. Treat `KC_AGENT` and policy allowlists as **accidental-misuse guards**, not same-UID security barriers. Actual isolation uses separately scoped platform tokens, OS identities where warranted, and provider permission boundaries.
3. Default to `kc mcp` metadata only. Opt-in `--allow-secret-values` is restricted to explicitly trusted clients and should not be enabled in shared agent templates; use `kc exec --require --only` for authenticated commands.
4. Scope every outbound push to a named platform + environment + repository + secret name, with a throwaway verification name and separately authorized real mutation. Do not retry 401 by widening permissions.
5. `keycli` owns secret resolution, injection, and target transport. `workspace-infra` owns host admission, queue/protection, evidence trust and PR/merge authority. Cloudflare (if adopted) hosts coordination metadata, not a second vault/authority engine.
6. Release binaries are pinned and integrity checked. A passing Linux source test is **not** a Mac GUI, native app, Cloud Agent runtime, real secret push, or provider E2E pass.

## 3. Provider contracts and acceptance

| Consumer | Default delivery | Owning verification | Status |
|---|---|---|---|
| Cursor local | Keychain/file via `kc exec --require --only` + optional helper | Live GUI and policy deny/allow + short-lived fake secret | PARTIAL |
| Cursor Cloud | Platform environment secret + `kc doctor`, scoped `kc exec` | Fresh VM, bound release SHA, exact permitted command, no leaked value | **COMPLETE for the allowlist contract** — single Linux VM, released `v0.7.2`, A/B/C = 0/5/2 with child spawn only on allow ([receipt](../reviews/2026-11-08-kc-cursor-cloud-p3-01.md)). `policy_digest` 재현, `pin_match`, `deny` 기능, 실제 provider 자격증명은 **NOT VERIFIED** |
| Claude Code local/cloud | Native platform secret or per-host local resolver; `CLAUDE.md` rules | One CLI turn with required non-secret output + deny/no-child scenario | NOT VERIFIED |
| Codex local/cloud | Native platform secret or per-host resolver; `AGENTS.md` rules | Same independent single-turn and phase-specific secret presence | NOT VERIFIED |
| OpenCode standalone | `kc exec` into `--standalone`; no background service secret persistence | One turn plus no `service.json` secret write | PARTIAL |
| Cloudflare Worker | Authorized `kc push cloudflare` to named test Worker | Wrangler stdin, API success, subsequent remove, free-budget/operation scope | NOT VERIFIED (fake Wrangler unit PASS) |
| GitHub Actions | Scoped `kc push gh --app actions`; Git helper for HTTPS | Independent set/delete receipt + actual auth signal | PARTIAL (KC-55 scoped real result) |

**Git HTTPS caveat:** `kc exec --only github -- git ls-remote` alone is not proof of credential use. The `kc setup-git` helper is required for this path; public-repository success proves nothing about authentication. See [workspace-infra synthetic review](https://github.com/camem2442/workspace-infra/blob/main/docs/reviews/2026-11-03-keycli-cloud-auth-evaluation.md).

## 4. Implementation phases / exact gates

### A. Local safe default (first)

- MCP default metadata-only, unauthorized direct `tools/call` denial, opt-in-only `get_secret`. **Source + v0.7.2 Linux release smoke COMPLETE**; real per-client deployment/admission still **NOT VERIFIED**.
- TUI fails closed without auto-clear; Cursor refuses live DB writes without a valid macOS restart path; isolated SQLite fixtures and real Mac receipt separate. **PR #17 source COMPLETE; Mac native NOT VERIFIED**.
- Run CLI status/exit regression, no values in stdout/stderr, no actual user vault touched by automated tests.

### B. Provider pilots (no blanket rollout)

- Select **one repository**, one role per provider, one synthetic nonproduction credential at a time.
- Record install/CLI/binary SHA, actual host, alias map, `middleware = ["audit","policy"]`, required names and policy allowlist.
- Required tests: allowed fake auth operation, forbidden name => exit 5 and child never spawned, missing name => exit 2 and child never spawned, audit outcome with metadata only, no token in output.
- Separate Cursor local/Cloud and Claude/Codex local/cloud cells; each must have its **own** run evidence. An installed CLI or claimed login is not acceptance.
- Handle `KC_KEY`/Keychain blocked in Mac sandbox by approved platform-native env or a separate scoped resolver, not by weakening sandbox or printing secrets.

### C. Transmission and lifecycle

- Cursor alias real PUT → list name → DELETE using a temporary secret: **COMPLETE** (2026-11-08, [receipt](../reviews/2026-11-08-kc-p4-cursor-alias-put.md)). v0.7.0 alias dry-run alone was not sufficient. `--repos` 범위, `secret_name_ambiguous`, 1000 상한, GUI 주입은 **NOT VERIFIED**.
- Cloudflare: first fake-Wrangler CI, then separate permissioned test Worker (no production rotation), then delete/verify. Confirm free-plan usage before repeated tests.
- Do not expand GitHub `--user`/`codespaces`/`agents` until a concrete consumer needs them.
- Add credential expiry/rotation/cleanup ownership: importing age bundles can persist file-store data without expiration; imported names may overwrite values. No automatic refresh or global sweep.

### D. Common verification integration

- Reuse [workspace-infra common verification](https://github.com/camem2442/workspace-infra/blob/main/docs/contracts/common-verification.md) and [canonical Goal PR consumer](https://github.com/camem2442/workspace-infra/blob/main/docs/contracts/canonical-goal-pr-verification-v1.md), not a second acceptance database.
- Suggested receipt: `{repo,pr,head_sha,base_sha,release_tag,binary_sha256,host_profile,platform,agent,credential_names_only,scope,gate_id,outcome,exit_code,run_id,policy_digest,source_receipt_digest}`. Fields are metadata only; producing a JSON file is **not** evidence authenticity.
- Preserve actual executing host ownership, exact HEAD and env/profile lock, supported host queue/guard, nonzero failures, and durable originals. A public file or client claim never yields authoritative PASS.
- Keep provider auth/write capability separate from Goal/Delivery permission: successful token lookup does **not** authorize push, deployment, approval or merge.
- If Cloudflare coordination metadata is later integrated, add bounded request budget, idempotency and explicit free-limit checks; R2/DO/Worker must not store raw credentials.
- **First live cross-repo synthetic pilot:** [workspace-infra PR #101](https://github.com/camem2442/workspace-infra/pull/101) ran the released keycli v0.7.2 Linux AMD64 binary on GitHub-hosted Ubuntu, with allowed/denied/missing `kc exec --require` cases and metadata-only [CI evidence](https://github.com/camem2442/workspace-infra/actions/runs/42319463989). Scoped synthetic host execution **COMPLETE**; canonical Goal authority, real Codex/Claude one-turn, and Cloudflare deployed transport **NOT VERIFIED**. Cloudflare remains offline shadow metadata only.

### E. Release + adoption

- Only propose v0.8.0 after Linux CI, Mac scoped acceptance, 1-turn Cursor/Claude/Codex owner receipts and a documented rollback path.
- Use v0.7.2 as the **new default install pin** after the verified patch release and this follow-up PR. Keep prior v0.7.1 available only for explicit rollback; do not claim safe-default MCP there. Real shared MCP client deployment and per-consumer pin changes still need their own host evidence; update consumer profiles only after separately accepted host evidence.
- Finish with a limited rollout to a single selected repository, then expand to other Develop Project repositories. Avoid "all projects enabled" claims based on one pilot.

## 5. Non-goals for v0.8 initial admission

Central vault, automatic export of all local keys, raw-secret MCP tools enabled by default, shared background service plaintext token sync, cross-account Cloudflare secrets replication, sweeping workspace credentials, and same-user agent isolation solely by `KC_AGENT`.

**Current goal status:** PARTIAL — source infrastructure exists; cross-provider operating acceptance and trusted delivery integration remain open.
