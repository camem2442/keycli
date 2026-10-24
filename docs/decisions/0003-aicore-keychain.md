---
status: accepted
date: 2026-10-24
---
# 0003 ai-core 키체인 읽기 어댑터

ai-core `keychain.py`와 동일한 규칙으로 **읽기 전용** 조회한다. 레거시 항목 자동 이전은 하지 않는다.

## kc 이름 → 조회

| kc 이름 | 의미 |
|---|---|
| `openai` | provider `openai`, 슬롯 1 (primary) |
| `groq#3` | provider `groq`, 슬롯 3 (`groq_api_key_3`) |

provider는 `gemini`, `groq`, `cerebras`, `openai`, `anthropic`만 ai-core로 해석한다.

## 키체인 (읽기)

- 서비스: `local.study.ai.<provider>` (provider 소문자)
- 계정: 슬롯 1 → `<provider>_api_key`, `<provider>_api_key_1` 순; 슬롯 n(2–10) → `<provider>_api_key_<n>`
- 레거시(gemini·groq): `local.study.lecturetranscriber.<provider>`, 계정 `<provider>_api_key` 또는 `<PROVIDER>_API_KEY`
- 호출: `security find-generic-password -s SVC -a ACCT -w` (비밀은 argv에 넣지 않음; kc는 `-w` 값 없이 stdin/`RunWithStdin` 사용)
- exit 44 → 없음(종료 2). 그 외 실패 → read error(종료 1 또는 3). `AI_CORE_NO_KEYCHAIN=1`이면 키체인 조회 생략.

## 환경변수 (ai-core와 동일 우선순위)

1. `<PROVIDER>_API_KEYS` (쉼표 구분, 첫 값)
2. `<PROVIDER>_API_KEY`

## kc 전용 쓰기 네임스페이스

사람용 `set`/`rm`은 `kc.secret.<name>` 서비스만 사용한다 (ai-core `local.study.ai.*`는 거부).

## 조정

provider 목록·접두사는 `[backend.work]`의 `service_prefix`와 config 확장으로 바꿀 수 있다(M2).
