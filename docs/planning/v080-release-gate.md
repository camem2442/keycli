---
status: current
owner: keycli
date: 2026-11-08
policy_basis: workspace-infra policies/testing-policy-linux-first.md policy_revision 2 (release stage)
---

# v0.8.0 release gate — common auth CLI stabilization

## Decision (2026-11-08, sole developer)

v0.8.0 is a **stabilization release of the common authentication CLI**. It is **not** the version at which every development agent is operationally accepted.

The earlier roadmap rule "only propose v0.8.0 after Linux CI, Mac scoped acceptance, 1-turn Cursor/Claude/Codex owner receipts and a documented rollback path" ([roadmap §4.E](v08-multi-agent-auth-infrastructure.md#e-release--adoption)) mixed two different stages. Under the [Linux-first testing policy](https://github.com/camem2442/workspace-infra/blob/main/policies/testing-policy-linux-first.md#release), a release verifies **its declared support scope and the actual published artifacts**; it does not absorb unrelated provider or full operating acceptance. This file records the split explicitly so that the old requirement is not silently dropped:

| Old v0.8.0 condition | New placement | Why |
|---|---|---|
| Linux CI on the exact source | **Release-blocking** | Existing `test` workflow, `./verify.sh --suite all` |
| Documented rollback path | **Release-blocking** | Previous pinned tag stays installable; installed binary kept |
| Published asset checksum + real binary smoke | **Release-blocking** (added) | Source tests do not replace artifact verification |
| Mac scoped acceptance | **Release-blocking only for the Mac binary smoke below**; GUI/Keychain/TUI/Cursor DB → consumer adoption | Those are native activation paths, not changed by this release |
| 1-turn Cursor / Claude / Codex owner receipts | **Consumer adoption** (per agent, when that agent is connected) | Model-provider E2E proves a consumer, not the CLI artifact |
| Cloudflare / GitHub other scopes / Cursor `--repos` | **Consumer adoption** | Separate external effects with their own approval |

Moving a condition to consumer adoption does **not** mark it COMPLETE. Every row in [roadmap §3](v08-multi-agent-auth-infrastructure.md#3-provider-contracts-and-acceptance) keeps its current status.

## Declared v0.8.0 scope

Already merged on `main` (no new feature code in this release):

1. Policy `allow`/`deny` per agent, deny-before-allow, malformed glob fail-closed (#30, #32, #33, #34, #36).
2. Alias ownership conflicts rejected before policy fingerprint and execution (#41).
3. Deterministic `policy_digest` in `kc policy show --json` ([contract](../contracts/effective-policy-digest-v1.md), #41).
4. `kc exec --require --only` — missing name exit 2, denied name exit 5, child never started.
5. `kc import` refuses to overwrite existing names unless `--force` (#39).
6. `kc mcp` metadata-only by default (since v0.7.2).
7. Existing transfer commands (`push cursor|gh|cloudflare`, `export`, `import`) stay CLI-compatible; no wire field removed.

## Release-blocking checks

| Check | Where | Evidence |
|---|---|---|
| `./verify.sh --suite all` on the exact tagged commit | existing `test` workflow, push to `main` | run id recorded in release evidence |
| Release assets + `SHA256SUMS` | existing `release` workflow on tag | published assets |
| Downloaded asset checksum matches `SHA256SUMS` | Mac, temporary directory | release evidence |
| Real binary synthetic core smoke (`kc-darwin-arm64`) | Mac, temporary prefix + temporary `HOME`/config, env backend only, no Keychain, no real store | release evidence |
| Rollback | `KC_INSTALL_REF=v0.7.2` + preserved previous binary | release evidence |

The synthetic core smoke covers: `--version`, `policy validate`, `policy show --json` digest stable across two runs, deny-hit and alias-conflict refusal, `exec --require --only` allow (child runs) / deny (exit 5, no child) / missing (exit 2, no child), `import` overwrite refusal, `mcp` tool list without `get_secret`.

`kc-linux-*` binaries are cross-compiled in the release job. Their Linux execution is covered by source CI only unless a separate Linux artifact smoke is recorded; cross-compilation is not claimed as Linux artifact execution.

## Out of scope (consumer adoption, not release-blocking)

- Per-agent connection: Cursor local, Claude Code, Codex, OpenCode, each with its own receipt when connected. The first pilot is the single most-used local agent.
- Mac GUI/Keychain/TUI and live Cursor DB acceptance ([matrix](v08-mac-tui-cursor-acceptance.md)).
- Cloudflare, GitHub other `--app`/`--user`, Cursor `--repos` scope.
- Changing the default install pin is a separate follow-up PR after the release evidence is recorded; publishing a tag does not change existing installs.
