# v0.7.0 release gate (GO/NO-GO)

**Status:** PARTIAL — Cloud Environment **Activate** on dashboard (build ready).

| Gate | Status |
|------|--------|
| Linux `verify.sh --suite all` (excl. mac-smoke on Linux) | COMPLETE (v0.7.0+) |
| `kc exec --require` | COMPLETE |
| KC-22 PR #11 | CLOSED — ADR 0006 |
| KC-57 cursor_api_key alias | COMPLETE on `main` (`c14354d`) |
| Cloud install build `bld-6404dc8d` | SUCCEEDED (v0.7.0); **Activate** user step |
| Mac mac-smoke / push cursor | COMPLETE (KC-55) |
| Acceptance matrix | PARTIAL (Codex/Claude NOT VERIFIED) |

Rollback: install `KC_INSTALL_REF=v0.6.0 bash scripts/install.sh`.
