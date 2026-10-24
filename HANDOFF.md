# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**M1 + Linux 테스트 강화** — fake `security`/clipboard, ai-core 읽기 어댑터, 통합 테스트, `verify.sh` suite 확장. **2026-10-24 실 Mac 검증·`security -i` 쓰기 수정·testhooks 게이트.**

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1 | **COMPLETE** | `./verify.sh --suite all` (macOS, 2026-10-24) |
| ai-core 읽기 (`<provider>`, `<provider>#n`) | **COMPLETE** (Linux/fake) | [0003](docs/decisions/0003-aicore-keychain.md), fake security 테스트 |
| fake security E2E | **COMPLETE** | `cmd/fakesecurity`, `KC_SECURITY` + `allow_test_hooks` 또는 `-tags=kc_testhooks` |
| age `KC_KEY` / file store | **COMPLETE** | 단위·통합·실 Mac `set`/`get demo` |
| age 키 macOS 키체인(`kc.age`) | **NOT VERIFIED** | 실 Mac에 `kc.age` 항목 없음; 키체인 쓰기 승인(-60006)으로 테스트 항목 생성 불가 |
| 클립보드 auto-clear | **COMPLETE** | 실 Mac `pbcopy`/`pbpaste`, `cp` auto-clear (`KC_CP_CLEAR_SECONDS=3`로 단축 확인; 기본 30s 경로 동일) |
| `set --clip` | **COMPLETE** | 실 Mac pbcopy/pbpaste, 저장 후 클립보드 비움 |
| 통합 시나리오 (set/get/ls/exec/cp/rm, 종료 코드) | **COMPLETE** | `internal/integration` |
| testhooks 게이트 | **COMPLETE** | `allow_test_hooks`, `-tags=kc_testhooks`; 릴리스 빌드는 `/usr/bin/security`·시스템 클립보드 고정 |
| **실 Mac 키체인 쓰기 (`security -i`)** | **NOT VERIFIED** | `add-generic-password` stdin은 macOS에서 비밀 저장 안 됨 → `ExecRunner`가 `security -i` + `-X` hex로 수정. **GUI/승인**: `kc set`·`security -i` 쓰기 시 `-60006` / 20s+ hang (무인 세션에서 승인 불가) |
| Keychain ACL / “항상 허용”·재빌드 | **NOT VERIFIED** | 쓰기·ACL 설정 불가; 읽기-only 무인 `get`은 groq 키체인 항목 부재로 ACL 시나리오 미실행 |
| 실 `/usr/bin/security` 읽기 (ai-core) | **NOT VERIFIED** | `dump-keychain`에 `local.study.ai.*` 없음; `env -i`에서 `kc get groq` → 종료 2. (에이전트 셸의 `GROQ_API_KEYS`는 키체인 검증과 분리) |
| 샌드박스 종료 3 | **NOT VERIFIED** (실 Mac) | `sandbox-exec` 프로필 `(deny user-interaction)` — macOS 27에서 unbound; fake `KC_FAKE_SECURITY_SANDBOX=1`로 Linux에서 종료 3 확인 |
| `link`, `push`, export/import/status | **DEFERRED** | roadmap |

## Linux 검증

```bash
./verify.sh --suite all
```

테스트 전용 env는 **`allow_test_hooks = true`** (config) 또는 **`go test -tags=kc_testhooks`** 빌드에서만: `KC_SECURITY`, `KC_TEST_KEYCHAIN`, `KC_CLIP_*`. 기타: `KC_FAKE_SECURITY_*`, `KC_CP_CLEAR_SECONDS`, `KC_CP_WAIT_CLEAR`.

## 실 Mac 수동 체크리스트 (2026-10-24)

| # | 항목 | 결과 |
|---|---|---|
| 1 | `KC_KEY` + `set`/`get demo` | **COMPLETE** |
| 2 | `env -i` + `kc get groq` (키체인) | **NOT VERIFIED** — 항목 없음(종료 2) |
| 3 | `kc cp demo` + pbcopy auto-clear | **COMPLETE** (3s 단축) |
| 4 | `kc set --clip` | **COMPLETE** |
| 5 | 샌드박스 `kc get` → 종료 3 | **NOT VERIFIED** — `sandbox-exec` 프로필 제한 |

**프롬프트**: 키체인 **쓰기** 시 승인 대화상자/ `-60006 The authorization was canceled by the user` — 무인 Cloud Agent 세션에서 응답 불가.

**정리**: `kc.test.*` 항목은 `dump-keychain`에 잔존 없음 (쓰기 승인 실패로 생성된 항목 없음).

## NEXT CONTEXT

M2: `link`, `kc doctor`, ai-core와 config provider 목록 동기화. 실 Mac에서 owner가 키체인 쓰기 승인 후 ACL·`kc.age`·ai-core 읽기 재검증 권장.
