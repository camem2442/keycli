---
status: historical
owner: keycli-delivery
---

# 운영 효과 승인 게이트 판정 (Phase S5)

S3(source)와 S4(읽기 전용) 결과 위에서 실제 provider 효과 허가 여부만 평가한다.
효과는 각각 독립 승인 대상이며, 하나의 승인이 다른 효과를 함의하지 않는다.

## 판정표

| 효과 | 요청 범위 | 판정 | 사유 |
|---|---|---|---|
| `CF_PILOT_BOOTSTRAP` | 비운영 Worker 1개 생성 | **NOT_AUTHORIZED** | Free 증거·계정 소유권·host binding 미확인 |
| `CF_PILOT_STAGE` | inactive secret version 1개 | **NOT_AUTHORIZED** | bootstrap 선행 미완료, deployment 불변 검증 대상 미확정 |
| `CF_PILOT_CLEANUP` | 정확한 Worker 1개 삭제 | **NOT_AUTHORIZED** | 생성 전이므로 삭제 대상 없음, 별도 승인 필요 |
| `PROVIDER_ISSUE_ONE` | Cloudflare/GitHub/OCI 1회 발급 | **NOT_AUTHORIZED** | 실제 계정·범위·예산 미확정 |
| `PROVIDER_REVOKE_EXACT_OLD` | 정확한 이전 세대 1개 폐기 | **NOT_AUTHORIZED** | provider-bound old identity 증거 미확보 |

`PROVIDER_MUTATION` 집계: **0회**. 이번 단계에서 실행한 provider 호출은 GET 포함 0회다.

## 게이트 7항목 점검

| # | 조건 | 상태 |
|---|---|---|
| 1 | 정확한 계정과 테스트 대상 | **미충족** — 계정 ID 미고정 |
| 2 | 공급자 Free/비용 근거 | **미충족** — HOLD |
| 3 | 현재 Writer와 Grant | **미충족** — KeyCLI Goal/Grant 미등록 |
| 4 | 비운영·최소 권한 실행 자격증명 | **미충족** — host binding 없음 |
| 5 | 실제 Effect와 실행 횟수 | **미확정** — 1회로 한정할 승인 필요 |
| 6 | 사후 검증·불확실 결과 재조정 | **부분** — journal `UNKNOWN_EFFECT`·reconcile 경로는 코드로 존재, 실계정 미검증 |
| 7 | 별도 승인된 정리·복구 경로 | **미충족** — cleanup 승인 없음 |

하나라도 누락이므로 provider mutation 없이 HOLD한다.

## 승인 판단에 필요한 최소 자료 (Owner용)

1. **계정 고정**: `CF_ACCOUNT_ID`(비밀 아님)와 host binding 이름
2. **Free 증거**: 대시보드 서명 증거(ed25519) 또는 entitlement API 응답 원문
3. **대상**: 신규 Worker 이름 규칙 `kc-pilot-<date>-<rand>` 1개, Worker URL 미호출
4. **자격증명**: Worker Edit + Workers Versions Write만 가진 일회성 토큰(계정 단위),
   만료 5분~24시간(#63 강제), 실행 후 즉시 폐기
5. **실행 예산**: bootstrap 1, stage 1, cleanup 1 (총 3회)
6. **중단 조건**: active deployment ID 변화, traffic weight 변화, 기존 Worker 목록 변화,
   UNKNOWN_EFFECT 발생 → 즉시 중단 후 reconcile
7. **정리 서명**: 별도 승인 문구와 실행자

## 금지 유지

- 동일 승인으로 bootstrap+stage+cleanup을 한 번에 실행하지 않는다.
- 실패 후 자동 재시도를 하지 않는다(`UNKNOWN_EFFECT`는 reconcile 먼저).
- 기존 Development Workers / MockTimer / #230 Auth Cutover Worker를 건드리지 않는다.
