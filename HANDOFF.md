# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**M2–M4 마일스톤:** `link`, `setup-git`, `rules`, `doctor`, mac-smoke ai-core 출처 검증, 에이전트 가이드.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1 | **COMPLETE** | v0.1.0 |
| `link` / `unlink` | **COMPLETE** | Linux integration + fake keychain |
| `get --source`, `ls --json` `source` | **COMPLETE** | resolver + mac-smoke keychain 출처 |
| `setup-git` / `git-credential` | **COMPLETE** | Linux dry-run/integration; Mac mac-smoke + `GIT_CONFIG_GLOBAL` |
| `rules` / `doctor` | **COMPLETE** | Linux tests |
| [docs/guides/agents.md](docs/guides/agents.md) | **COMPLETE** | |
| Linux CI | **COMPLETE** | `./verify.sh --suite all` |
| mac-smoke (Darwin) | **NOT VERIFIED** | Linux CI skip; owner Mac에서 `--suite mac-smoke` |
| 샌드박스 종료 3 (실 Mac) | **NOT VERIFIED** | sandbox-exec 프로필이 항상 차단하지 않음 |
| 에이전트 실사용 (Cursor/Claude/Codex) | **NOT VERIFIED** | |
| `push`, `export`, `import`, `status` | **DEFERRED** | M4.5 |

## 검증

```bash
./verify.sh --suite all
./verify.sh --suite mac-smoke   # Darwin: KEYCHAIN_WAIT=90, scrubbed env for ai-core
```

**mac-smoke:** 실제 `HOME` + `KC_CONFIG` 격리. ai-core 단계는 `env -i`(셸의 `GROQ_API_KEYS` 등 제거) + `kc get groq --source`로 `keychain:` 출처 확인.

## NEXT CONTEXT

M4.5: `push`, `export`/`import`, `status`. 실 Mac에서 mac-smoke 재실행 및 에이전트별 `kc rules --write` 1회.
