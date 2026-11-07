---
status: implemented
contract: set-time-verify
version: 1
---
# Set-time verify (v1)

`kc set`은 저장 직전에 등록된 **provider**에 맞는 원격 검증을 수행한다. 검증은 방금 입력한 값만 메모리에서 사용하며 `kc get`을 호출하지 않는다.

## 동작

| 플래그 | 의미 |
|---|---|
| (기본) | provider가 있으면 verify 실행 |
| `--no-verify` | 검증 생략 |
| `--force` | 검증 실패 시에도 저장 진행 |

검증 실패 시 **저장 중단**(종료 1), `--force`가 있으면 중단하지 않는다. 부분 프로브 경고는 stderr 한 줄로 알리고, narrow 기준을 만족하면 저장은 계속한다.

## 출력 (stdout만)

비밀 바이트·응답 본문 전체·토큰 문자열을 **출력하지 않는다**. 허용되는 stdout:

1. `provider <id> verify_status <http>`
2. 선택: `cf_codes […]` (Cloudflare errors[].code)
3. 프로브 요약: JSON **한 줄당 하나** (`probe`, `status`, `result`, 선택 `code`/`count`)

## Provider v1

이름 또는 `--note`가 provider id와 **정확히 일치**하거나, 이름이 `cloudflare_<suffix>`이고 아래 표에 있으면 적용한다.

| Provider | 트리거 |
|---|---|
| `cloudflare_agent` | 이름 `cloudflare_agent` 또는 `cloudflare_agent` 노트 |
| `cloudflare_billing_read` | 이름 `cloudflare_billing_read` 또는 동일 노트 |
| `github_pat` | 이름 `github_pat` (v1: 검증 no-op, 예약) |

### `cloudflare_agent`

1. `GET https://api.cloudflare.com/client/v4/user/tokens/verify` — `Authorization: Bearer` (입력값)
2. verify HTTP 200·`success`이면 `GET …/accounts` — **계정 개수만** 기록 (`count`, 응답 본문 비출력)

### `cloudflare_billing_read`

1. 동일 `tokens/verify`
2. 200이면 계정 `CF_ACCOUNT_ID`(기본 `b398955bcfe917a4af61d179af967b6a`)로 프로브:

| probe | 경로 | 기대 |
|---|---|---|
| `subscriptions` | `/accounts/{id}/billing/subscriptions` | 200 → narrow 필수 |
| `billable_usage_v1` | `/accounts/{id}/billing/v1/billable-usage` | 200 → narrow 필수 |
| `user_billing_profile` | `/user/billing/profile` | 실패 시 경고( narrow 통과 시 저장 허용) |
| `spend_policies` | `/accounts/{id}/billing/spend-policies` | 404 또는 CF code 1003 → `not_found`(auth 실패 아님) |

**narrow 통과:** `subscriptions`와 `billable_usage_v1` 모두 `ok`. 그 외 프로브는 경고만.

테스트·CI: `CF_API_BASE`로 API 베이스 덮어쓰기. 실연동은 `KC_SET_VERIFY_INTEGRATION=1` 및 `CF_API_TOKEN`.

## 금지

- verify 경로에서 `kc get`
- 검증 응답 본문을 저장소·로그에 보관
- GitHub PAT 등으로 타 API 데이터 유출
