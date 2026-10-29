# v0.7.0 release gate (GO/NO-GO)

**Status:** COMPLETE — v0.7.0 출시 + operational close (2026-10-29). 범위: 아래 표의 게이트에 한정. 근거: [v07-operational-evidence.md](./v07-operational-evidence.md).
남은 인수(이 게이트 범위 밖): Codex/Claude 1턴 **NOT VERIFIED**, 에이전트+정책 1회 **NOT VERIFIED**, KC-59 alias 경유 실 PUT **NOT VERIFIED**, KC-25(gh 401, 재현 시).

| Gate | Status |
|------|--------|
| Linux `verify.sh --suite all` (excl. mac-smoke on Linux) | COMPLETE (v0.7.0+) |
| `kc exec --require` | COMPLETE |
| KC-22 PR #11 | CLOSED — ADR 0006 |
| KC-57 cursor_api_key alias | COMPLETE on `main` (`c14354d`) |
| KC-59 post-merge alias smoke (Mac) | COMPLETE — alias 해석 + `kc push cursor --dry-run`만; alias 경유 실 PUT은 NOT VERIFIED |
| Cloud install build `bld-6404dc8d` | SUCCEEDED + Activate; fresh VM smoke COMPLETE (KC-40) — see [v07-operational-evidence.md](./v07-operational-evidence.md) |
| Mac mac-smoke / push cursor (실 PUT·목록·DELETE) | COMPLETE (KC-55) — see [v07-operational-evidence.md](./v07-operational-evidence.md) |
| Mac `kc push gh` (`camem2442/keycli`, `--app actions`) | COMPLETE (KC-55, dry-run/set/delete); 다른 `--app`·`--user` 범위는 NOT VERIFIED |
| Acceptance matrix | PARTIAL (Codex/Claude NOT VERIFIED) |

Rollback: install `KC_INSTALL_REF=v0.6.0 bash scripts/install.sh`.
