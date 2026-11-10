---
status: BLOCKED
owner: keycli / workspace-infra
date: 2026-11-10
scope: read-only audit + proposed disposable pilot admission
---

# Cloudflare K09-04B Free-only 파일럿 — Billing Read 없이 조건부 인수

**현재 판정: READ_ONLY_PREFLIGHT_COMPLETE / LIVE_PILOT_HOLD.** 이 문서는 실제 Worker/토큰을 생성·삭제하거나 Free 요금 청구 결과를 확인한 영수증이 아니다.

## 실제 계정 조회 (2026-11-10 KST)

- 대상은 기존 Development Infrastructure 계정 한 곳. account ID는 사전 등록된 신뢰된 호스트 바인딩에서만 사용하고 로그·문서에 원문 인증 토큰을 기록하지 않는다.
- `GET /accounts`, `GET /accounts/{account_id}/workers/scripts`, `GET /accounts/{account_id}/workers/account-settings`: PASS. 등록 Worker **6개**, 신규 `kc-v09-*` 파일럿 Worker **0개**. `default_usage_model=standard`; 이것은 **요금제 Free 증거가 아니다**.
- `GET /accounts/{account_id}/entitlements`: HTTP 200, 배열 **0개**. 빈 entitlement는 Free 증빙이 아니다.
- `GET /accounts/{account_id}/subscriptions`: **403** (Billing Read 부재). 이전 읽기 전용 감사에서 Billing Usage, Billable Usage Info 및 Spend Policy도 403. 현재 사용량·유료 서비스·무료 플랜 상태를 provider read-only API만으로 인증할 수 없다.
- 기존 6개 Worker는 개발/서비스 이전에 사용 중이다. MockTimer 운영 계정, 기존 6개 Worker, #230 Auth Cutover Worker의 동시 쓰기를 **금지**한다.

## 공개 Free 조건을 이용한 최소 비용 봉투

[Cloudflare Workers Free 제한](https://developers.cloudflare.com/workers/platform/limits/) (2026-10-29 문서 기준): **일일 Worker 요청 100,000회, 계정당 Worker 100개, 요청 CPU 10 ms**. 신규 한 개를 추가하면 Worker 숫자는 7개로 계정당 100개 제한 안에 들지만, **해당 계정이 Workers Free인지/이미 사용한 요청 수가 얼마인지 이 숫자만으로는 확인할 수 없다**. CPU/요청/추가 기능 실사용을 0이라고 단정해서도 안 된다.

### Billing Read 없는 대체 Gate

| 구간 | 소유 근거 | 실패 시 |
|---|---|---|
| A: Free 플랜 증명 | 실제 Cloudflare Dashboard에서 계정 ID·Workers Free·추가 유료 기능 비활성·조회 날짜를 소유자 신뢰 채널로 확인/보존. Owner 보고라면 Provider API 독립 증명이 아니라 `OWNER_ATTESTED_SCOPED`로만 표기 | Free 상태 불명 → **HOLD** |
| B: Provider inventory | 정확한 계정 GET + Worker 전체 목록 (지금 6개) + 기존 cutover Writer/Goal 소유. 새 Worker 이름이 기존과 충돌하지 않음 | account mismatch/Writer conflict → **BLOCKED** |
| C: 제품 범위 | 새 고유 disposable Worker 1개. `workers_dev=false`, route/custom domain/cron/assets/R2/D1/KV/DO/service binding/Observability 유료 설정 **없음** | 금지 바인딩/트래픽 노출 → **BLOCKED** |
| D: bounded operation budget | 1 bootstrap upload/deploy, 1 inactive `versions secret put`, 읽기 GET 최대 20회, 1 owner-authorized cleanup. 합성 Secret 1개·최대 1회 시도·재시도 0 | 예산/요금 불확실 또는 중복 시도 → **HOLD** |
| E: 실제 effect 권한 | workspace-infra current Goal/Writer Claim, 별도 Cloudflare 최소 Worker write scope, Owner의 생성·정리 각각 독립 grant. read-only token에서 쓰기 scope로 자동 fallback 없음 | 별도 허가 없으면 **HOLD** |
| F: 사후 관측 | before/after 같은 account/Worker의 deployment/version/traffic weights, Stage는 비활성 1개만 생성, secret 값은 stdout/log 금지 | 인수 불명 → `UNKNOWN_EFFECT`, 재PUT 금지 |

공식 Free 가격표는 **퍼블릭 요금·한도 정보**일 뿐 계정별 실제 구독/청구를 인증하지 않는다. Owner Free 플랜 확인만으로도 보유 Billing Read 권한이 생기는 것은 아니며, 빈 Entitlements·기본 usage_model도 증거가 아니다.

## 단계별 실제 파일럿 허용 한계

- **Phase 0:** 위 A~C를 읽기 전용으로 확인. 계정/플랜/Writer/Free 증빙 불충분한 현재는 여기에서 종료.
- **Phase 1:** Owner가 별도로 승인한 최초 일회성 Worker bootstrap. 이것은 실제 Deployment effect이며 K09-04 Stage 허가로 대체할 수 없다. 설계·버전·로그·권한을 미리 고정한다.
- **Phase 2:** before snapshot 후 정확한 `wrangler versions secret put` 단일 호출, operation journal, `workers/tag`/Script Version binding의 두 GET 및 active weights 불변 확인. `wrangler secret put`·`wrangler versions deploy` **호출 0회**.
- **Phase 3:** Owner가 별도로 승인한 생성 리소스·토큰 정리. 유효한 Cleanup 권한이 없으면 파일럿을 시작하지 않는다. 폐기 전 원본 증거를 보존하고 남은 Worker/token list를 확인한다.

**성공 정의:** `OPERATING_ACCEPTED_DISPOSABLE`(해당 한 개 disposable Worker에만). 기존 Developer/MockTimer Worker 운영 배포, 계정 전체 Free billing 검증, OCI 또는 GitHub 토큰 회전에 대한 권한을 발행하지 않는다.

## 남은 blocker 및 소유자 결정

- 현재 **Subscriptions/Billing Usage 403**: 같은 권한으로 재시도/권한 확대하지 않는다.
- 실제 Dashboard Workers Free와 부가 유료 서비스 상태의 최신 확인, account usage total/동시 작업, 최소 권한 쓰기+정리 grant가 필요하다.
- Provider 쓰기/배포·토큰 발급·무료 요금제 전환은 이번 문서 범위에 없다.

출처: [Workers Limits](https://developers.cloudflare.com/workers/platform/limits/) · [Workers Pricing](https://developers.cloudflare.com/workers/platform/pricing/) · [Worker Secrets](https://developers.cloudflare.com/workers/configuration/secrets/) · [K09-04B disposable plan](11-cloudflare-disposable-worker-pilot.md).

## Free-only Admission 코드 — #59 (Billing Read 미보유 상태)

[Draft #59](https://github.com/camem2442/keycli/pull/59)의 `internal/delivery/freegate`는 **미구현 운영 권한이 아닌 읽기 전용 판정**이다. 실제 계정 ID를 호스트에서 별도로 고정하고, 별도 보관된 Ed25519 Owner 공개키로 **24시간 이내의 Workers Free dashboard 증언**을 검증한 뒤, 신뢰된 provider GET으로 가져온 현재 Worker 목록이 서명 대상과 정확히 같을 때만 `OWNER_ATTESTED_SCOPED`를 반환한다. 결과는 항상 `authoritative=false`, `authorizes_execution=false`.

- Development Infrastructure 계정에 기존 Worker **6개**를 조회했다. `Subscriptions` 및 `Billing Usage`는 403, `Entitlements`는 조회 성공/빈 목록, `default_usage_model=standard`. 이 값들은 계정이 Free인지, 남은 요청·과금 한도가 충분한지 증명하지 않는다. 신규 Pilot Worker 생성 없음.
- Cloudflare 공식 Workers Free **100 Worker/account, 요청 100,000/day**는 정책 상한이며 계정의 실제 결제 계약과 당일 잔여량이 아님.
- 별도로 Owner가 인증한 Free dashboard 증거/권한 한도/독립 Writer·정리 권한이 없는 현재 **실계정 파일럿은 HOLD**. `FreeGate`를 통과하더라도 새 `cloudflaredelivery.Run`을 자동으로 호출할 수 없고, 별도의 workspace-infra Goal/effect admission이 요구된다.
- [#61 Go 1.22.12 CI](https://github.com/camem2442/keycli/actions/runs/42787409731)는 Gate의 합성 서명·틀린 계정/서명/비용/목록 테스트를 통과했다. 이것은 Cloudflare Free Plan proof가 아니다.

[Gate implementation guide](https://github.com/camem2442/keycli/blob/feat/keycli-k09-freegate-owner-attestation-20261110/docs/guides/cloudflare-freegate-signed-pilot.md).
