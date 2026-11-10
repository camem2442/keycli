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

1. `plan`: 계정 alias·Worker·환경·source alias·대상 이름을 정적 검증한다. 값 조회·인증·Provider API·Wrangler 실행은 0회이며 실제 배포 기준과 권한은 미확인으로 남긴다.
2. `admit`: WI-KC-01이 연결된 workspace-infra 신뢰 호스트가 exact Worker/account/operation과 Grant·비용·단일 writer를 결속한다. 실제 Cloudflare credential과 Wrangler config 범위도 확인한다.
3. `pre-observe`: 활성 deployment ID/버전/weight와 최신 업로드 버전 ID를 별도 기준으로 읽는다. 계정·Worker·환경, 격리 바인딩, `preview_urls=false`와 `workers_dev=false`를 확인한다. 불일치 또는 미확인이면 쓰기를 막는다.
4. `INTENT_WRITTEN`: 두 기준값, operation ID, source alias, destination, Grant와 Plan digest를 private journal에 durable 기록한다. 저장·잠금 실패 시 쓰기를 막는다.
5. `stage`: 쓰기 직전 두 원격 기준과 preview 설정을 재조회한다. 일치하면 pin된 Wrangler의 `versions secret bulk`를 한 번 호출하며 bounded secret bytes는 stdin으로 전달한다. `--name`, `--env`, `--config`와 실행 CLI 버전을 고정한다.
6. `post-observe`: API에서 새 후보가 이 작업의 최신 업로드 버전인지, 후보에 필요한 Secret 이름이 있는지 확인한다. 활성 deployment ID/버전/weight와 preview 설정은 그대로여야 한다. 쓰기 뒤 상태나 인과관계가 불명확하면 `UNKNOWN_EFFECT`로 보류한다.
7. `receipt`: 공급자 version ID와 호스트가 확인한 source·대상·관측 결속을 기록한다. 값, raw token, Vault 디렉터리, 값 digest는 기록하지 않는다.
8. `promotion`: workspace-infra의 별도 Delivery 효과가 fresh 검증·권한·비용과 정확한 활성 기준을 확인한 뒤 `versions deploy`를 실행한다.

Wrangler CLI 출력은 안정된 증명 API로 가정하지 않는다. 버전 ID가 출력에서 파싱되더라도 실제 API GET으로 교차 확인한다. 원격 쓰기 타임아웃/결과 미기록은 성공 확정이 아니므로 반드시 `UNKNOWN_EFFECT`, 재시도 전에 관측한다.

## 다중 Secret / 동시 변경

v0.9 Stage는 하나 또는 여러 Secret을 한 번의 bulk version 생성 시도로 묶는다. 계정/Worker/환경 단위의 협력 writer fence를 사용한다. Wrangler 경로에는 기대 기준값에 대한 공급자 CAS 보장이 없으므로, 쓰기 직전 재조회에서 활성 또는 최신 업로드 기준이 바뀌면 `STALE`로 종료한다. 외부 writer의 변경을 배제할 수 없으면 운영 Stage를 보류한다. Auth Cutover의 writer도 같은 대상에 포함한다.

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
| 합성 Stage 단일 | `versions secret bulk` 1회, 일반 `secret put`과 배포 호출 0 |
| 합성 Stage 다중 | `versions secret bulk` 1회, stdin JSON, 평문 파일 0 |
| 원격 증거 | 새 최신 업로드 version ID와 작업 결속, 활성 deployment/버전/weight와 preview 설정 그대로 |
| 동시 stage / wrong account | STALE 또는 BLOCKED, 새 effect 0 |
| timeout 후 replay | GET/reconcile 선행, 무조건 재전송하지 않음 |
| Version URL | 격리 리소스 또는 부작용 없는 진단만 |
| 비용 | 승인 범위·Free 한도 확인되지 않으면 라이브 호출 보류 |

## 공식 출처

- https://developers.cloudflare.com/workers/wrangler/commands/workers/
- https://developers.cloudflare.com/workers/configuration/secrets/
- https://developers.cloudflare.com/workers/versions-and-deployments/
- https://developers.cloudflare.com/workers/versions-and-deployments/gradual-deployments/
