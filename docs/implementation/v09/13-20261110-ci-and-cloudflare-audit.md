---
status: PARTIAL
owner: keycli
date: 2026-11-10
scope: exact-source CI and connected Cloudflare read-only preflight; no operating authorization
---

# 2026-11-10 Go 1.22 gate · Development Cloudflare read-only audit

## A. 신규 소스 통합 CI — COMPLETE_SCOPED

- [검증 전용 KeyCLI PR #57](https://github.com/camem2442/keycli/pull/57) HEAD `60dca53541ba847b0caba64b840d79e2d5ebaf22`, base [#54](https://github.com/camem2442/keycli/pull/54) `76fb6afdb389ea22833d9f84cd160485878b166a`.
- 기존 #54에서 Go 1.22 전체 테스트를 통과한 소스에 **정확히 7개 Git blob**만 추가: [#55](https://github.com/camem2442/keycli/pull/55) Cloudflare Script/Worker Version GET 2개, [#56](https://github.com/camem2442/keycli/pull/56) 정적 키 폐기 전 실제 Provider identity 재확인 5개.
- [GitHub Actions run #42774640356](https://github.com/camem2442/keycli/actions/runs/42774640356): run_attempt 1, event pull_request, head `60dca53541ba847b0caba64b840d79e2d5ebaf22`, Linux job `SUCCESS`; job log `go version go1.22.12 linux/amd64`; `./verify.sh --suite all` 실행·성공.
- #57은 **DO NOT MERGE** 원본/통합 검증 전용. #55와 #56 owner PR은 독립 branch·HEAD 상태를 유지하며, CI 성공이 인수·공급자 권한·운영 키 폐기 허가가 되지 않는다.
- Go 1.23.2 Linux 임시 module/fake Journal 집중 테스트·race도 PASS했으나 보조 증거다. 정확한 Go 1.22 검증 근거는 이번 #57이다.

## B. Cloudflare Development Infrastructure 계정 읽기 전용 관측

2026-11-10 connected Cloudflare API GET. 실제 Worker Secret 값·API 토큰은 조회/저장하지 않았다.

- Development Infrastructure 계정 이름·신원 조회 성공. Workers Script 목록 **6개**:
  `context-platform-gateway-p1`, `context-platform-study-auth-alpha`, `context-platform-study-p5-alpha`, `gene-pedigree`, `ios-control-plane-staging`, `sogang-wallet-pass`.
- **K09 일회성 disposable Worker는 현재 목록에 없음.** 기존 여섯 Worker 중 하나를 Pilot으로 재사용하지 않는다.
- 여섯 Worker 모두 최신 Deployment GET 성공; 각 최신 배포는 버전 하나에 트래픽 100%라고 보고. 실제 앱 가용성·인증까지 확인한 것은 아니다.
- Workers Account Settings GET 성공: `default_usage_model=standard` 및 `green_compute=false`. **이 속성은 Billing Free/Paid entitlement·잔여 무료 한도·현재 청구 금액을 증명하지 않는다.**
- `GET /accounts/{id}/subscriptions`: HTTP **403**.
- `GET /accounts/{id}/billing/usage`: HTTP **403**, Billing Read scope 없음.
- `GET /accounts/{id}/billable-usage/info`: HTTP **403**.
- `GET /accounts/{id}/billing/spend-policies`: HTTP **403**.
- Account Token verify GET은 HTTP **401**; 다른 Workers GET 성공 사실과 구별한다. 이 연결의 OAuth 자격증명을 Account API Token으로 오인하지 않으며, mutation 권한을 주장하지 않는다.

**판정: CLOUDFLARE_DISPOSABLE_LIVE = HOLD / COST_NOT_VERIFIED / STAGE_PERMISSION_NOT_VERIFIED**.
실제 plan·과금·비용 상한·권한이 소유자/공급자 원본으로 확인되고, 기존 [Auth Cutover #230](https://github.com/camem2442/workspace-infra/pull/230)의 active writer 충돌이 없어야 다음 단계로 간다.

## C. 제안하는 변경 불가 사전 조건

1. 새 disposable Worker 정확한 계정/Worker identity, `workers_dev=false`, route/cron/DB/KV/DO/R2/서비스 바인딩 없음. 최초 bootstrap deploy도 별도 effect 승인.
2. read-only Worker GET token과 단일 Worker에 제한된 stage token을 분리; 운영 토큰/기존 Secret 재사용 금지. Token lifecycle issuer/revoker는 별도 Owner 소유.
3. 사용된 Wrangler 버전·실제 `--name/--env` script binding, Script Version 및 Worker Version API에 대한 조회 권한과 response shape를 먼저 확인.
4. 비용은 실제 Billing Free entitlement 및 잔여/과금 정책을 확인하지 못하면 $0 보장을 금지; 소스만으로 quota 여유나 전환 권한을 추론하지 않는다.
5. 유일한 허용된 stage effect는 `wrangler versions secret put` 1회; `wrangler secret put`, `versions deploy`, `deploy`, 기존 Worker 타깃은 금지.
6. Before/After Deployment ID·traffic weight 불변과 태그/바인딩의 동일 Version 관측이 가능할 때만 scoped stage 검증. 비확실 결과는 `UNKNOWN_EFFECT`로 남고 재PUT 금지.
7. 별도 정리 권한으로 테스트 Worker/Token을 회수했음을 확인하기 전 Operating Complete 금지.

## D. 조건부 병합·릴리스 순서 (이번에 병합 없음)

- [#46](https://github.com/camem2442/keycli/pull/46)은 문서-only 독립.
- 구현 owner 경로: **#47 → #48 → #50 → #55** (Cloudflare) 및 **#47 → #49 → #51 → #52 → #53 → #56** (정적 키 회전).
- 검증 전용 #54와 #57은 **DO NOT MERGE**. 검증된 source snapshot·원본 raw run/step 증거를 보존하고, 실제 병합 커밋·릴리스 산출물·Mac 설치는 각 경계를 다시 확인한다.
- [workspace-infra #251](https://github.com/camem2442/workspace-infra/pull/251)은 Goal/Claim 소비자 연결 문서-only; 실행 adapter와 운영 승인은 미구현.
- v0.9.x source integration은 **COMPLETE_SCOPED**; WIF/GitHub Secret 실제 provider, Cloudflare Stage 실계정, OCI Instance/Runner, K10 발급/폐기 실전 인수는 **NOT VERIFIED/HOLD**.

## 공식 참고
- [Cloudflare Workers Pricing](https://developers.cloudflare.com/workers/platform/pricing/)
- [Cloudflare Secrets: put vs versions secret put](https://developers.cloudflare.com/workers/configuration/secrets/)
- [Cloudflare Get Worker Version](https://developers.cloudflare.com/api/resources/workers/subresources/beta/subresources/workers/subresources/versions/methods/get/)
