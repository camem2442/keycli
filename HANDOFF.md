# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**M1 + Linux 테스트 강화** — fake `security`/clipboard, ai-core 읽기 어댑터, 통합 테스트, `verify.sh` suite 확장.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1 | **COMPLETE** | Linux `verify.sh --suite all` |
| ai-core 읽기 (`<provider>`, `<provider>#n`) | **COMPLETE** | [0003](docs/decisions/0003-aicore-keychain.md), fake security 테스트 |
| fake security E2E | **COMPLETE** | `cmd/fakesecurity`, `KC_SECURITY`, argv 로그 검사 |
| age `KC_KEY` / file store | **COMPLETE** | 단위·통합 테스트 |
| age 키 macOS 키체인(`kc.age`) | **PARTIAL** | `SecurityRunner` 경로 구현; fake security로 Linux 테스트 |
| 클립보드 auto-clear | **COMPLETE** | 단위 테스트 + `KC_CP_WAIT_CLEAR` 통합; 프로덕션은 `_clip-wait-clear` 자식 프로세스 |
| 통합 시나리오 (set/get/ls/exec/cp/rm, 종료 코드) | **COMPLETE** | `internal/integration` |
| **NOT VERIFIED (실 Mac만)** | | |
| Keychain ACL / “항상 허용” 프롬프트 | **NOT VERIFIED** | fake 불가 |
| 실 `/usr/bin/security`·NSPasteboard·pbcopy | **NOT VERIFIED** | Linux fake로 대체 검증 |
| `link`, `push`, export/import/status | **DEFERRED** | roadmap |

## Linux 검증

```bash
./verify.sh --suite all
./verify.sh --suite integration
./verify.sh --suite keychain
```

환경 변수(테스트): `KC_SECURITY`, `KC_TEST_KEYCHAIN=1`, `KC_FAKE_SECURITY_*`, `KC_CLIP_*`, `KC_CP_CLEAR_SECONDS`, `KC_CP_WAIT_CLEAR`.

## 실 Mac 수동 체크리스트 (5)

1. `kc keygen` → `KC_KEY` 또는 키체인 `kc.age`/`kc` 항목에 저장 후 `kc set demo` / `kc get demo`
2. `kc get groq` — ai-core `local.study.ai.groq` primary 키 읽기(읽기 전용; `kc set groq` 거부)
3. `kc cp demo` — 클립보드 붙여넣기 후 ~30초 뒤 자동 삭제
4. `kc set x --clip` — pbcopy/pbpaste 경로, 저장 후 클립보드 비움
5. 샌드박스 터미널(Codex/Claude Code)에서 `kc get` — 키체인 실패 시 종료 3과 stderr

## NEXT CONTEXT

M2: `link`, `kc doctor`, ai-core와 config provider 목록 동기화.
