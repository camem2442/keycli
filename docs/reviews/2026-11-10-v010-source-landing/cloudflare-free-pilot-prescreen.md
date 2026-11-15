---
status: historical
owner: keycli-delivery
---

# Cloudflare Free-only 파일럿 사전검사 (2026-11-10)

## 판정

```
CF_READONLY_AUDIT = NOT_VERIFIED   (승인된 host binding·자격증명 없음 → GET 0회)
CF_FREE_PROOF     = HOLD
CF_WRITER_AUTHORITY = HOLD
CF_BOOTSTRAP      = NOT_AUTHORIZED
CF_STAGE          = NOT_AUTHORIZED
CF_CLEANUP        = NOT_AUTHORIZED
```

## 왜 읽기 전용 조회조차 하지 않았는가

이번 작업에는 Cloudflare 계정에 접근할 수 있는 **승인된 호스트 바인딩이나
자격증명 경로가 없다.** `~/.kc`의 실제 항목, Keychain, `KC_KEY`, 운영 토큰을 읽지
않는다는 불변식이 그대로 적용된다. 따라서 아래 "선행 검사"는 **수행하지 않았고**,
과거 관측값(Workers 6, Billing 403, entitlements empty)을 현재값으로 재사용하지도
않는다.

## 코드 수준에서 확인한 것 (provider 접촉 없음)

| 항목 | 근거 | 결과 |
|---|---|---|
| `wrangler secret put`을 쓰지 않는가 | `internal/target/cloudflare/version_stage.go`는 `wrangler versions secret put` 경로만 사용 | 확인 |
| active deployment/traffic weight 무변경 검사 | `version_observe.go` `validWeights`, deployment ID·버전 집합 비교 | 확인 |
| Version tag를 Wrangler 출력이 아니라 독립 API로 검증 | #55 `TestVersionTagNeedsIndependentWorkerVersionAPIProof` | 확인 |
| Free-only 서명 게이트 | `delivery/freegate/gate.go` ed25519 + 계정 ID 고정 + 실 재고 일치 | 확인 |
| Free 증거 없으면 provider read 없이 차단 | `TestMissingOrForgedFreeEvidenceBlocksWithoutProviderRead` | 확인 |
| 불확실 응답 자동 재시도 없음 | `TestVersionStageUnknownEffectAndRedaction`, journal `UNKNOWN_EFFECT` | 확인 |

즉 **코드 경로는 fail-closed**이지만, 실제 계정의 Free 플랜·권한·재고는 증명되지 않았다.

## 실행 전 필요한 선행 검사 (승인된 호스트에서만)

| # | 검사 | 통과 기준 | 현재 |
|---|---|---|---|
| 1 | 계정 소유권 | 지정 Development Infrastructure 계정 ID 일치 | 미확인 |
| 2 | Worker 전체 목록 | 조회 시점 목록이 서명 증거와 정확히 일치 | 미확인 |
| 3 | Free 플랜 증거 | 계정별 entitlement 또는 대시보드 서명 증거 | 미확인 |
| 4 | 유료 부가 기능 | Workers Paid/D1/R2/KV/DO 등 유료 바인딩 0 | 미확인 |
| 5 | Writer 소유권 | 동일 target 동시 Writer 없음(claim generation) | 미확인 |
| 6 | 권한 범위 | 최소 권한(비운영 Worker 1개, secret 1개) | 미확인 |

`default_usage_model=standard` 또는 빈 entitlement는 **Free 증명이 아니다.**

## Disposable Worker 설계 (승인 시 적용)

| 제약 | 값 |
|---|---|
| 신규 Worker | 1개, 기존 Worker 덮어쓰기 금지 |
| routes / custom domain / cron | 0 |
| D1 / R2 / KV / DO 바인딩 | 0 |
| production traffic | 0 |
| bootstrap operation | 1회 |
| inactive secret version | 1개 (`wrangler versions secret put`) |
| 관측 | before/after read-only, active deployment ID + traffic weights 비교 |
| cleanup | 별도 승인 후 정확한 Worker 1개 삭제 |

Version URL은 연결 리소스 부작용 가능성이 있으므로 비운영 Worker 외에는 호출하지 않는다.

## Free-only Gate 항목과 현재 상태

| 항목 | 요구 | 현재 |
|---|---|---|
| `owner_attestation_fresh` | 서명 증거 유효기간 내 | 미확인 |
| `account_id_exact` | 계정 ID 고정 일치 | 미확인 |
| `worker_inventory_exact` | 조회 목록 == 서명 목록 | 미확인 |
| `plan_scope_exact` | Free 범위 정확 | 미확인 |
| `free_entitlement_evidence` | 계정별 증거 존재 | 미확인 |
| `no_paid_bindings` | 유료 바인딩 0 | 미확인 |
| `cleanup_authority` | 정리 권한 별도 승인 | 미확인 |

서명 검증은 출처·내용 일치만 확인하며 공급자 청구 결과를 보증하지 않는다.
모든 읽기 전용 검사가 통과해도 `authoritative=false`,
`authorizes_execution=false`를 유지한다.

## 다음 승인 대상

1. Cloudflare 읽기 전용 조회용 host binding (계정 ID 고정, 최소 권한 토큰)
2. Free 플랜 증거 확보 방법 결정(대시보드 서명 증거 vs entitlement API)
3. 위 1–6이 통과한 뒤에만 `CF_PILOT_BOOTSTRAP` 단일 승인
