# v0.7.0 release gate (GO/NO-GO)

**Status:** PARTIAL — pending Mac cursor push + promotable Cloud env build.

| Gate | Status |
|------|--------|
| Linux `verify.sh --suite all` (excl. mac-smoke on Linux) | NOT VERIFIED at tag time |
| `kc exec --require` | COMPLETE (code + integration) |
| KC-22 PR #11 | CLOSED — standalone default per ADR 0006 |
| Cloud install on `main` | PARTIAL — scripts merged; Dashboard Save user action |
| Acceptance matrix | PARTIAL — see Project `consumer-profile-matrix.md` |
| `kc push cursor` API | NOT VERIFIED (Mac API key) |

Rollback: install `KC_INSTALL_REF=v0.6.0 bash scripts/install.sh`.
