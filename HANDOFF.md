# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**M4.5 마일스톤:** `push`, `export`/`import`, `init`, 원장·`status`, phase hint.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1–M4 | **COMPLETE** | v0.2.0 |
| `push cursor` / `push gh` | **COMPLETE** (단위) | httptest / fake `gh`; **실 API·실 gh: NOT VERIFIED** |
| `export` dotenv / age / `--to-github` | **COMPLETE** (단위) | 클립보드·age round-trip 테스트; **실 GitHub.keys: NOT VERIFIED** |
| `import` / `init` | **COMPLETE** | Linux 테스트 |
| 원장·`status`·`ls` stale | **COMPLETE** | ledger + file store HMAC key |
| env 종료 2 phase hint | **COMPLETE** | `get` |
| Linux CI | **COMPLETE** | `./verify.sh --suite all` |
| mac-smoke (Darwin) | **NOT VERIFIED** | Linux skip |
| Cursor Cloud 실 push | **NOT VERIFIED** | |
| 에이전트 실사용 | **NOT VERIFIED** | |

## 검증

```bash
./verify.sh --suite all
./verify.sh --suite mac-smoke   # Darwin
```

## NEXT CONTEXT

M5: 외부 `kc-target-*` / `kc-backend-*` 프로토콜. 실 Mac에서 mac-smoke 및 `kc push cursor` 1회.
