# v0.7.0 release gate (GO/NO-GO)

**Status:** GO for v0.7.0 operational scope — see [v07-operational-evidence.md](./v07-operational-evidence.md).

| Gate | Status |
|------|--------|
| Linux `verify.sh --suite all` (excl. mac-smoke on Linux) | COMPLETE (v0.7.0+) |
| `kc exec --require` | COMPLETE |
| KC-22 PR #11 | CLOSED — ADR 0006 |
| KC-57 cursor_api_key alias | COMPLETE on `main` (`c14354d`) |
| Cloud install build `bld-6404dc8d` | SUCCEEDED + Activate; fresh VM smoke COMPLETE (KC-40) — see [v07-operational-evidence.md](./v07-operational-evidence.md) |
| Mac mac-smoke / push cursor | COMPLETE (KC-55); post-merge alias KC-59 — see [v07-operational-evidence.md](./v07-operational-evidence.md) |
| Acceptance matrix | PARTIAL (Codex/Claude NOT VERIFIED) |

Rollback: install `KC_INSTALL_REF=v0.6.0 bash scripts/install.sh`.
