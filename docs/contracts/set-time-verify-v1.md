---
status: implemented
contract: set-time-verify
version: 1
---
# Set-time verify (v1)

`kc set`은 저장 직전에 등록된 **provider**에 맞는 원격 검증을 수행한다. 검증은 방금 입력한 값만 메모리에서 사용하며 `kc get`을 호출하지 않는다.

**시크릿 바이트:** 기본은 입력값을 **그대로** 저장·검증에 사용한다(의도적인 앞뒤 공백·개행 보존). 파이프 stdin만 끝의 개행 **한 번**을 제거하고, `--clip`은 클립보드 텍스트 끝의 `\n`만 제거한다. 붙여넣은 PAT 등 단일 줄 토큰에서 CR/LF·공백을 치우려면 `--normalize`를 쓴다(앞뒤 공백 trim + 본문의 모든 CR/LF 제거). merge 전 Mac에서의 우회: `kc set NAME --clip`, 또는 `printf '%s' "$TOKEN" | kc set NAME`, 필요 시 `--normalize` 또는 `--no-verify` 후 수동 확인.

## 동작

| 플래그 | 의미 |
|---|---|
| (기본) | provider가 있으면 verify 실행 |
| `--normalize` | 저장·set-time verify 직전에 단일 줄 PAT 정규화(trim + CR/LF 제거) |
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
| `github_agent` | 이름 `github_agent`, 동일 노트, 또는 `github_<suffix>` (표: `agent`) |
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

### `github_agent`

1. `GET https://api.github.com/user` — `Authorization: Bearer` (입력값). HTTP **200** 필수; `login`만 프로브 JSON에 기록.
2. 토큰 verify 통과 시 probe repo (환경 변수로 덮어쓰기 가능):

| 변수 | 기본값 |
|---|---|
| `GH_PROBE_REPO` | `camem2442/mock-exam-timer` |
| `GH_PROBE_BRANCH` | `integration/member-records` |

| probe | 경로 | 기대 |
|---|---|---|
| `branch` | `/repos/{repo}/branches/{branch}` | 200 → `protected` 불리언만 |
| `rulesets` | `/repos/{repo}/rulesets` | 200 → 통과 (빈 배열 허용); **403 → 저장 거부** (admin read 부족) |
| `protection` | `…/branches/{branch}/protection` | `protected=true`일 때만 호출; 200 → `required_status_checks.contexts` **이름만** (최대 50); 404 → `not_found` (실패 아님) |

**통과:** `/user` 200 + `rulesets` 403 아님. `rulesets` 403은 `--force` 없이 **저장 중단**.

## 테스트·CI

- Cloudflare: `CF_API_BASE` 덮어쓰기; 실연동 `KC_SET_VERIFY_INTEGRATION=1` + `CF_API_TOKEN`.
- GitHub: `GH_API_BASE` 덮어쓰기; probe repo/branch는 `GH_PROBE_REPO` / `GH_PROBE_BRANCH`. 실연동은 등록 PAT만 메모리에서 사용(argv·로그 금지).

## 금지

- verify 경로에서 `kc get`
- 검증 응답 본문을 저장소·로그에 보관
- GitHub PAT 등으로 타 API 데이터 유출
