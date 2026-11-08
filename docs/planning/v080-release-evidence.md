# v0.8.0 release evidence — common auth CLI stabilization

**Status:** COMPLETE for the [release gate](v080-release-gate.md) scope only (Linux source CI + published assets + Mac `kc-darwin-arm64` synthetic smoke). Consumer adoption rows in the [roadmap](v08-multi-agent-auth-infrastructure.md#3-provider-contracts-and-acceptance) keep their status.
**Tag:** [v0.8.0](https://github.com/camem2442/keycli/releases/tag/v0.8.0) → `ad306079d8d756bebdc2de28f76cf18b69a1f911` (lightweight; merge of #43).
**Date:** 2026-11-08.

## Release-blocking checks actually run

| Check | Result | Identity |
|---|---|---|
| `./verify.sh --suite all` on the tagged commit | PASS | `test` push run [42467385847](https://github.com/camem2442/keycli/actions/runs/42467385847), head `ad30607` |
| Existing `release` workflow (tag push) | PASS, 6 assets | run [42467541594](https://github.com/camem2442/keycli/actions/runs/42467541594) |
| Downloaded `kc-darwin-arm64`, `kc-linux-amd64` vs published `SHA256SUMS` | OK | Mac, temp dir |
| Synthetic core smoke, downloaded `kc-darwin-arm64` | 16/16 PASS | Mac, temp `HOME`/`KC_CONFIG`, env + temp file store, no Keychain |
| `scripts/install.sh` with `KC_INSTALL_REF=v0.8.0 PREFIX=<temp>` + same smoke | PASS | curl path returned HTTP 504; installer fell back to `gh release download`, sha256 verified |

Smoke cases: `--version` = `v0.8.0 (ad30607)`; `policy validate`; `policy_digest` identical on two runs; `exec --require --only` allow (child ran, synthetic value present), deny (exit 5, no child, no value on stderr), missing (exit 2, no child), unconfigured agent → default deny (exit 5); duplicate alias owner → `policy validate` and `exec` refused; `import` of an existing name refused without `--force`; `kc mcp` default tools = `doctor, list_secrets, policy_check`; audit log contains no values.

Published SHA256 (from `SHA256SUMS`):

- `kc-darwin-arm64` `fd84b597510a2c70d49954cd29210ca74a297afdbcafc6ef7d326fbdf5b385ff`
- `kc-darwin-amd64` `08e876ba15e0d074744dc9125e352d67f302ae3687d7d0c3df4509e83dea68be`
- `kc-linux-amd64` `7010d11bbf05b9db30cc299055e417eb371ea9c8781e66c68807a5c9b215f517`
- `kc-linux-arm64` `0deead8712c50fb0a154740c7081bdd34bc9a6ac385d7e40c80fdb822aafbd8a`
- `keycli-linux-amd64` `1f66cc070fe4ae6e51d6819e044bc2bf80d6c42bc10d9d94694c8a329ed3bfdd`
- `SHA256SUMS` `7614b04b08a31f2259da0c1427d03071bb5d9e24bc920c3d0b24aa1e49c4f048`

**NOT VERIFIED:** execution of `kc-linux-*` / `kc-darwin-amd64` artifacts (cross-compiled; Linux behaviour covered by source CI only), Mac Keychain/GUI/TUI, any model-provider turn, Cloudflare/GitHub transfers.

## Mac installation (one path)

- Active path: `~/.local/bin/kc` (`~/bin/kc` and `~/.local/bin/keycli` are symlinks to it). Replaced by atomic rename with the smoke-tested binary; `kc --version` = `v0.8.0 (ad30607)`, SHA256 matches the published asset.
- Previous binary preserved: `~/.local/libexec/kc-rollback/kc-v0.7.2-50-g513414b` (SHA256 `5b705519…958b`). Rollback: copy it back, or `KC_INSTALL_REF=v0.7.2 bash scripts/install.sh`.
- Before replacement, the real config passed `kc policy validate` with v0.8.0 (no values read).

## First consumer pilot — Cursor local (2026-11-08)

Selection: the local audit log showed Cursor as the most frequent `kc` consumer (and active during the switch). One agent only; Claude Code, Codex and OpenCode are connected when needed.

- Config change (backed up first): a human-only wallet credential added to `deny` for the `codex`, `claude` and `cursor` sections. `opencode` has no section and stays on `default` (`allow = []`).
- With the installed v0.8.0 and the real config, `KC_AGENT=cursor`: deny → exit 5, no child; missing synthetic name → exit 2, no child; allowed synthetic env name → exit 0, child ran. `kc policy check` (no values): codex/claude/cursor `deny_hit`, opencode `not_allowed`, human allowed.
- No `kc get`, vault transfer or env dump was used.
- **NOT VERIFIED:** a Cursor model turn performing a task through `kc`, Cursor Cloud on v0.8.0 (Cloud default pin stays `v0.7.2`), operating-effect tier narrowing (deferred until the running Cloudflare cutover closes).

## Install pin

`scripts/install.sh` default → `v0.8.0` (this PR). `scripts/cloud-install.sh` stays `v0.7.2` until a Cursor Cloud VM accepts v0.8.0. Publishing the tag changed no installs by itself.
