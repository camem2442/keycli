# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**M5 마일스톤:** 외부 `kc-backend-*` 플러그인 프로토콜 v1, config 명시 활성화, `kc plugin list`, 예제 플러그인·Linux 테스트.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1 | **COMPLETE** | v0.1.0 |
| `link` / `unlink` | **COMPLETE** | Linux integration + fake keychain |
| `get --source`, `ls --json` `source` | **COMPLETE** | resolver + mac-smoke keychain 출처 |
| `setup-git` / `git-credential` | **COMPLETE** | Linux dry-run/integration; Mac mac-smoke + `GIT_CONFIG_GLOBAL` |
| `rules` / `doctor` | **COMPLETE** | Linux tests |
| M5 external plugins | **COMPLETE** | [plugin-protocol.md](docs/contracts/plugin-protocol.md), [0004](docs/decisions/0004-external-backend-plugin.md) |
| [docs/guides/agents.md](docs/guides/agents.md) | **COMPLETE** | |
| Linux CI | **COMPLETE** | `./verify.sh --suite all` |
| mac-smoke (Darwin) | **NOT VERIFIED** | Linux CI skip; owner Mac에서 `--suite mac-smoke` |
| 샌드박스 종료 3 (실 Mac) | **NOT VERIFIED** | sandbox-exec 프로필이 항상 차단하지 않음 |
| 에이전트 실사용 (Cursor/Claude/Codex) | **NOT VERIFIED** | |
| `push`, `export`, `import`, `status` | **DEFERRED** | M4.5 |
| 서드파티 플러그인 (1Password 등) 실연동 | **NOT VERIFIED** | 예제 `kc-backend-demo`만 검증 |

## 검증

```bash
./verify.sh --suite all
./verify.sh --suite mac-smoke   # Darwin: KEYCHAIN_WAIT=90, scrubbed env for ai-core
```

**mac-smoke:** 실제 `HOME` + `KC_CONFIG` 격리. ai-core 단계는 `env -i`(셸의 `GROQ_API_KEYS` 등 제거) + `kc get groq --source`로 `keychain:` 출처 확인.

## NEXT CONTEXT

M4.5: `push`, `export`/`import`, `status`. 실 Mac에서 mac-smoke 재실행. 서드파티 `kc-backend-*` 작성 시 [plugin-protocol.md](docs/contracts/plugin-protocol.md) 준수.
