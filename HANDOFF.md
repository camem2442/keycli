# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**실 Mac mac-smoke** — 키체인 쓰기/ACL, `kc.age`, ai-core 읽기. v0.1.0 설치 검증.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1 | **COMPLETE** | `./verify.sh --suite all` (Linux) |
| Linux CI (fake security/clip/integration) | **COMPLETE** | `-tags=kc_testhooks` |
| 설치·릴리스 | **COMPLETE** | v0.1.0, `~/.local/bin/kc` |
| keychain 쓰기 (`security -i`, `-T /usr/bin/security`) | **COMPLETE** | 2026-10-24 mac-smoke, `kc.test.*` |
| 키체인 재읽기·kc 재빌드 후 읽기 | **COMPLETE** | mac-smoke, 재프롬프트 없음 |
| ai-core `kc get groq` (키체인) | **COMPLETE** | mac-smoke, len만 기록 (값 미출력) |
| `kc.age` 로드·file store round-trip | **COMPLETE** | throwaway `kc.age` 생성 후 삭제 |
| mac-smoke `HOME` 규칙 | **COMPLETE** | 실제 `HOME` + `KC_CONFIG`; 임시 `HOME` 금지 |
| 샌드박스 종료 3 | **NOT VERIFIED** | `sandbox-exec` 프로필이 차단하지 않음 (exit 0); Linux fake는 종료 3 |
| `link`, `push`, … | **DEFERRED** | roadmap |

## 검증

```bash
./verify.sh --suite all
./verify.sh --suite mac-smoke   # Darwin: 설치된 kc, KEYCHAIN_WAIT=90 권장
```

**mac-smoke 주의**: macOS가 `~/Library/Keychains`를 찾으려면 **실제 사용자 `HOME`**이 필요합니다. 예전 스크립트의 임시 `HOME`은 **「키체인 발견할 수 없음」** 대화상자를 냈습니다. 설정만 `KC_CONFIG`로 격리하고 `KC_KEY`는 unset 합니다.

## NEXT CONTEXT

M2: `link`, `kc doctor`. 샌드박스 exit 3은 Codex/Claude Code 터미널에서 수동 재현 권장.
