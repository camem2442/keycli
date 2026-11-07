---
status: implemented
contract: set-time-verify
version: 1
---
# Set-time verify (v1)

`kc set`은 저장 직전에 등록된 **provider**에 맞는 원격 검증을 수행한다. 검증은 방금 입력한 값만 메모리에서 사용하며 `kc get`을 호출하지 않는다.

**시크릿 바이트 보존:** 기본은 입력값을 그대로 사용한다. stdin은 마지막 개행 한 번만 제거하고 `--clip`은 끝의 `\\n`을 제거하는 기존 동작을 유지한다. 단일 줄 PAT의 붙여넣기 공백·CR/LF를 제거하려면 명시적으로 `--normalize`를 사용한다. 일반 시크릿·여러 줄 키에는 사용하지 않는다.

## 동작

| 플래그 | 의미 |
|---|---|
| (기본) | provider가 있으면 verify 실행 |
| `--normalize` | 단일 줄 API 토큰에서만 명시적으로 앞뒤 공백 및 CR/LF 제거 |
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
| `subscriptions` | `/accounts/{id}/subscriptions` | 200 → narrow 필수 |
| `billable_usage_v1` | `/accounts/{id}/billable-usage` | 200 → narrow 필수 |
| `user_billing_profile` | `/user/billing/profile` | 실패 시 경고( narrow 통과 시 저장 허용) |
| `spend_policies` | `/accounts/{id}/billing/spend-policies` | 200 + `result: []`는 접근 성공, 404/1003은 `not_verified` 경고(빈 목록이나 정책 존재 여부를 증명하지 않음) |

**narrow 통과:** `subscriptions`와 `billable_usage_v1` 모두 HTTP 200, `success=true`, `result` 배열을 반환해야 한다. 다른 프로브의 실패는 경고로 유지하되, Free 요금제·청구 주기·Spend Policy 강제·0원 운영·Staging 배포를 증명하지 않는다. `user_billing_profile`은 `result` 객체, `spend_policies`의 HTTP 200은 `result` 배열이어야 한다.

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
| `protection` | `…/branches/{branch}/protection` | `protected=true`일 때만 호출; 200 → `required_status_checks.contexts` **이름만** (최대 50); 401/403·전송 오류·잘못된 응답 → 저장 중단; 404 → `not_found` 경고 (권한 확인 아님) |

**통과:** `/user` 200, branch와 유효한 `rulesets` 응답을 확인한다. 보호된 브랜치에서 classic protection 조회가 401/403 또는 오류면 `--force` 없이 저장을 중단한다. 브랜치가 비보호이거나 classic rule이 404인 경우 저장은 허용하되 경고하며, branch protection 읽기 권한을 확인했다고 주장하지 않는다. ruleset에 의해 보호된 브랜치에는 classic rule이 없을 수 있다.

## 테스트·CI

- 운영 검증 대상은 각각 `https://api.cloudflare.com/client/v4` 및 `https://api.github.com`으로 고정하고 HTTP redirect를 따르지 않는다. `CF_API_BASE`와 `GH_API_BASE`가 공식 주소가 아닌 값으로 설정돼 있으면 외부 요청 전에 오류로 중단한다. 환경 변수의 원문은 오류에 출력하지 않는다.
- 테스트는 메모리 내 HTTP transport를 주입하며 실제 서버나 비밀을 사용하지 않는다. Cloudflare 실연동은 `KC_SET_VERIFY_INTEGRATION=1` + `CF_API_TOKEN`이 별도로 필요한 선택 검사다. GitHub probe repo/branch는 `GH_PROBE_REPO` / `GH_PROBE_BRANCH`로 지정한다.

## 운영 인수 경계

- 실제 Secret 값은 로그·CI artifact·PR·Shadow 관측값에 기록하지 않는다. `--force` 또는 `--no-verify`로 저장하더라도 인증/권한 인수를 의미하지 않는다.
- 기존 AGENTS 네트워크 불변식은 `kc set`의 원격 검증 기능과 별도 정합성 승인이 필요하다. 이 통합 Draft는 정책 변경·실제 토큰 호출·Cloudflare Staging 배포를 승인하지 않는다.

## 금지

- verify 경로에서 `kc get`
- 검증 응답 본문을 저장소·로그에 보관
- GitHub PAT 등으로 타 API 데이터 유출
