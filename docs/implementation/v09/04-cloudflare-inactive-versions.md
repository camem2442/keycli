---
status: NOT VERIFIED
scope: proposed-cloudflare-staging
---

# Cloudflare Worker 시크릿 — stage ≠ deploy

## 금지될 기존 경로

현재 `internal/target/cloudflare/cloudflare.go`의 `Push`는 `wrangler secret put NAME`을 실행한다. 이 명령은 새 Worker Version을 **만들고 즉시 배포**한다. 기존 코드가 이미 운영 Worker를 안전하게 보호한다고 가정하지 않는다. v0.9는 새로운 `kc delivery stage`로 안전 경로를 추가하되, 구버전 `kc push cloudflare`에 운영 가드를 소급했다고 주장하지 않는다.

- `wrangler versions secret put`: 새 **비활성** Worker Version 생성.
- `wrangler versions secret bulk`: 여러 Secret 변경을 **한 버전 후보**로 생성; JSON을 stdin으로 전달 가능, 평문 temp file 금지.
- `wrangler versions deploy`: **실제 트래픽 전환**. KeyCLI Stage adapter가 호출하면 계약 위반.
- Version URL의 테스트도 D1/KV/R2/DO 등 실제 바인딩에 부작용을 줄 수 있다. 분리한 테스트 리소스와 무해한 GET 프로브가 아니면 운영 Version URL 실험 금지.

## Stage 단계의 정확한 프로토콜 (PROPOSED)

1. `plan`: 계정 alias·Worker 이름·wrangler config·env·대상 비밀 이름 검증; 값 조회·Provider API·CLI 실행 없음. 완료를 PASS로 판단하지 않는다.
2. `admit`: workspace-infra가 exact Worker/account/operation에 대해 제한된 쓰기 허가와 단일 Writer 소유권을 인수; 실제 Cloudflare credential 범위를 독립적으로 제한.
3. `pre-observe`: Read API에서 현재 활성 deployment ID, 버전 및 각 weight, worker binding/required secrets 정보를 조회. 계정·리소스 신원이 다르면 중단. 조회 못하면 `NOT VERIFIED`.
4. `write intent`: 예상 기준 버전, operation-id, source alias(값 없음), destination, auth scope, 계획 digest를 원장에 durable 기록. 잠금 오류면 원격 쓰기 금지.
5. `stage`: bounded secret bytes를 stdin으로 `wrangler versions secret put/bulk` 실행. 인수 시 검증된 Wrangler 버전을 pin. `--name`, `--env`, `--config` 범위 정확성 및 CLI flag 지원 확인.
6. `post-observe`: Cloudflare API에서 새 version ID와 실제 secret 이름 존재, 변경 전후 활성 배포·트래픽 가중치 **불변** 확인. 바뀌었으면 `BLOCKED/UNKNOWN_EFFECT`이며 절대 STAGED 확정 금지.
7. `receipt`: provider가 보고한 version ID와 host-verified source 결속만 기록. secret value, raw token, vault 디렉터리, secret digest 출력 금지.
8. `promotion`: 별도 workspace-infra Delivery/Owner가 최신 검증·권한·비용 게이트를 통과시킨 다음 `versions deploy`. KeyCLI는 promotion 실행권을 기본적으로 보유하지 않음.

Wrangler CLI 출력은 안정된 증명 API로 가정하지 않는다. 버전 ID가 출력에서 파싱되더라도 실제 API GET으로 교차 확인한다. 원격 쓰기 타임아웃/결과 미기록은 성공 확정이 아니므로 반드시 `UNKNOWN_EFFECT`, 재시도 전에 관측한다.

## 다중 Secret / 동시 변경

기존 동작처럼 `for secret { secret put }`을 반복하면 여러 version과 예상치 못한 활성 상태 변경을 야기한다. v0.9 Stage는 명시적 배열을 원자적인 **한 번의 bulk version 생성 시도**로 묶는다. Cloudflare 계정/Worker/환경 잠금을 작업 단위로 독점; 변경 중 다른 Agent나 기존 Auth Cutover가 Worker를 수정하면 `STALE`. Wrangler가 기대 base version을 CAS로 고정할 수 없다면 최신 상태를 재조회하고 동시 쓰기가 가능한 호스트에서는 운영 Stage를 거부한다.

## 기존 Development cutover와의 충돌 금지

[workspace-infra #230](https://github.com/camem2442/workspace-infra/pull/230)은 별도 Development Auth Cutover 계획이다. 2026-11-08 보고된 14/15 staged 기록은 역사적 증거로만 보존한다. 이 기능의 합성 파일럿은 **별도 일회성 Worker**에서 검증한다. 다섯 기존 Worker, MockTimer 운영 계정, OAuth/Access, 보유 Secret에는 접근·배포·삭제하지 않는다. cutover Goal의 Writer 락이 있으면 변경 자체가 `BLOCKED`다.

## Cloudflare API Token 회전

- 운영 Token은 scope 최소화. 새 Token 준비 → 제한된 비운영 GET/권한 확인 → 새 경로를 사용하는 실행 호스트 검증 → 이전 Token 폐기.
- Token을 `Roll`해서 즉시 폐기하는 것은 선행 consumer migration 검증 없이는 금지.
- Editor Token은 자신을 DELETE하지 못하는 사례가 KeyCLI 파일럿에 있었음. 정리할 발급 주체의 별도 권한을 확인하되 광범위 운영 자격증명을 에이전트에 제공하지 않음.
- Worker Secret과 **배포용 Cloudflare API Token**은 서로 다른 credential life cycle이다. Worker Secret 준비를 API Token 회전 완료라고 표현하지 않는다.

## 인수 테스트

| 확인 | 기대 |
|---|---|
| `plan` / `dry-run` | Keychain/Resolver 값 접근 0, wrangler 호출 0 |
| 합성 Stage 단일 | `versions secret put`만 실행, `secret put/deploy` 0 |
| 합성 Stage 다중 | `versions secret bulk` 1회, stdin JSON, 평문 파일 0 |
| 원격 증거 | 새 version ID 관측, 활성 ID/weight 그대로 |
| 동시 stage / wrong account | STALE 또는 BLOCKED, 새 effect 0 |
| timeout 후 replay | GET/reconcile 선행, 무조건 재전송하지 않음 |
| Version URL | 격리 리소스 또는 부작용 없는 진단만 |
| 비용 | 승인 범위·Free 한도 확인되지 않으면 라이브 호출 보류 |

## 공식 출처

- https://developers.cloudflare.com/workers/wrangler/commands/workers/
- https://developers.cloudflare.com/workers/configuration/secrets/
- https://developers.cloudflare.com/workers/versions-and-deployments/
- https://developers.cloudflare.com/workers/versions-and-deployments/gradual-deployments/
