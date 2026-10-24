# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**M4.5 + M5:** transfer(`push`/`export`/`import`/`status`)와 외부 `kc-backend-*` 플러그인 프로토콜 v1·`kc plugin list`가 main에 합류 대기 중(PR #5).

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1–M4 | **COMPLETE** | v0.2.0 / v0.3.0 |
| `link` / `unlink` | **COMPLETE** | Linux integration + fake keychain |
| `get --source`, `ls --json` `source` | **COMPLETE** | resolver + mac-smoke keychain 출처 |
| `setup-git` / `git-credential` | **COMPLETE** | Linux dry-run/integration; Mac mac-smoke + `GIT_CONFIG_GLOBAL` |
| `rules` / `doctor` | **COMPLETE** | Linux tests |
| `push cursor` / `push gh` | **COMPLETE** (단위) | httptest / fake `gh`; **실 API·실 gh: NOT VERIFIED** |
| `export` dotenv / age / `--to-github` | **COMPLETE** (단위) | 클립보드·age round-trip 테스트; **실 GitHub.keys: NOT VERIFIED** |
| `import` / `init` | **COMPLETE** | Linux 테스트 |
| 원장·`status`·`ls` stale | **COMPLETE** | ledger + file store HMAC key |
| env 종료 2 phase hint | **COMPLETE** | `get` |
| M5 external plugins | **COMPLETE** | [plugin-protocol.md](docs/contracts/plugin-protocol.md), [0004](docs/decisions/0004-external-backend-plugin.md) |
| [docs/guides/agents.md](docs/guides/agents.md) | **COMPLETE** | |
| Linux CI | **COMPLETE** | `./verify.sh --suite all` |
| mac-smoke (Darwin) | **NOT VERIFIED** | Linux CI skip; owner Mac에서 `--suite mac-smoke` |
| 샌드박스 종료 3 (실 Mac) | **NOT VERIFIED** | sandbox-exec 프로필이 항상 차단하지 않음 |
| Cursor Cloud 실 push | **NOT VERIFIED** | |
| 에이전트 실사용 (Cursor/Claude/Codex) | **NOT VERIFIED** | |
| 서드파티 플러그인 (1Password 등) 실연동 | **NOT VERIFIED** | 예제 `kc-backend-demo`만 검증 |

## 검증

```bash
./verify.sh --suite all
./verify.sh --suite mac-smoke   # Darwin
```

**mac-smoke:** 실제 `HOME` + `KC_CONFIG` 격리. ai-core 단계는 `env -i`(셸의 `GROQ_API_KEYS` 등 제거) + `kc get groq --source`로 `keychain:` 출처 확인.

## NEXT CONTEXT

외부 `kc-target-*` 프로토콜(DEFERRED). 실 Mac에서 mac-smoke·`kc push cursor` 1회. 서드파티 `kc-backend-*`는 [plugin-protocol.md](docs/contracts/plugin-protocol.md) 준수.
