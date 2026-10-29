# v0.7.0 release gate (GO/NO-GO)

**Status:** COMPLETE — v0.7.0 출시 + operational close (2026-10-29). 남은 인수: Codex/Claude 1턴 **NOT VERIFIED**, KC-25(gh 401, 재현 시).

| Gate | Status |
|------|--------|
| Linux `verify.sh --suite all` (excl. mac-smoke on Linux) | COMPLETE (v0.7.0+) |
| `kc exec --require` | COMPLETE |
| KC-22 PR #11 | CLOSED — ADR 0006 |
| KC-57/59 cursor_api_key alias | COMPLETE on `main` (`c14354d`) |
| Cloud install build `bld-6404dc8d` + Activate | COMPLETE (KC-40) |
| Mac mac-smoke / push cursor | COMPLETE (KC-55) |
| Acceptance matrix | PARTIAL (Codex/Claude NOT VERIFIED) |

Rollback: install `KC_INSTALL_REF=v0.6.0 bash scripts/install.sh`.
