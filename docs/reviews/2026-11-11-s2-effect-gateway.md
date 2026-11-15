---
status: historical
owner: delivery
last_verified_commit: c713384
---

# S2 — 단일 Effect Gateway 구현·검증 (2026-11-11)

## 기준

| 항목 | 값 |
|---|---|
| keycli 기준 HEAD | `c713384` (tree `658875c4`) |
| workspace-infra | `606e3ed` |
| 설계 근거 | `effect-fencing-decision-v1.md` |
| Lima VM | `kc-isolation-20261111` — **Stopped 유지** (시작하지 않음) |

## 재사용 매트릭스

| 요구 | 기존 구현 | 신규 작성 |
|---|---|---|
| durable intent / 중복 방지 / target latch | `journal.Store.Dispatch` | 없음 (호출만) |
| `UNKNOWN_EFFECT` 래치 / reconcile 전용 | `journal` | 없음 |
| effect 직전 admission | `hostbridge.Bridge.Authorize` | 없음 |
| claim generation/session/lease | `SqliteClaimStore` (Python) | `ClaimAuthority` 인터페이스(어댑터는 호스트 소유) |
| 요청 인증 | 없음 | **신규**: 세션별 HMAC 키 + canonical payload |
| 실행 직렬화 | journal `flock` | 없음 (호출만) |
| provider 호출 단일 경로 | 없음 | **신규**: `Provider` 인터페이스 + `Submit` |

신규 파일 3개 + journal에 검증 함수 2개(6줄)만 추가했다.

## 최소 diff

| 파일 | 변경 |
|---|---|
| `internal/effectgateway/contract.go` | 신규 |
| `internal/effectgateway/gateway.go` | 신규 |
| `internal/effectgateway/gateway_test.go` | 신규 (14 테스트) |
| `internal/delivery/journal/journal.go` | `ValidOperationID`, `ValidDigest` export (+6줄) |
| `docs/contracts/effect-gateway-v1.md` | 신규 |

## 상태 전이표

| 입력/사건 | journal | receipt |
|---|---|---|
| 정상 1건 | `ACKNOWLEDGED` | `APPLIED` (1 call) |
| 동일 mutation 재전송 | 변화 없음 | `DUPLICATE` (0 calls) |
| 서명 위조/세션 교체 | 기록 없음 | `ErrUnauthenticated` |
| stale generation | 기록 없음 | `ErrStaleGeneration` |
| lease 만료 / 비ACTIVE | 기록 없음 | `ErrClaimLeaseExpired` / `ErrClaimNotActive` |
| owner 불일치 | 기록 없음 | `ErrOwnerMismatch` |
| admission 거부 | 기록 없음 | `ErrAdmissionRejected` |
| effect 경계 재검증 실패 | `OBSERVED_NO_EFFECT` | `NO_EFFECT` (0 calls) |
| provider 응답 유실 | `UNKNOWN_EFFECT` | `UNKNOWN_EFFECT` (1 call) |
| authority 조회 실패 | **기록 없음** | `ErrAuthorityUnavail` |

## Synthetic provider ledger

| 테스트 | provider calls | 비고 |
|---|---|---|
| 정상 | 1 | `ACKNOWLEDGED` |
| 중복 | 1 (누적) | 2번째 요청 0 |
| 동시 8건 | **1** | applied=1, 나머지 duplicate/busy |
| crash 후 재시도 | 0 | journal에 이미 기록 |
| 응답 유실 | 1 | 재시도해도 1 |
| effect 경계 재검증 | 0 | claim 변경 감지 |
| authority 실패 | 0 | journal 미기록 |

## 테스트 환경·결과

| 항목 | 결과 |
|---|---|
| `go test ./internal/effectgateway/` (Go 1.22.12) | **14 PASS** |
| keycli 전체 `go test -tags=kc_testhooks ./...` | **40/40 패키지 ok** |
| `go vet` (effectgateway, journal) | ok |
| `verify.sh --suite docs` / `policy` | ok / ok |
| infra `verify.py --suite keycli-* --suite authority` (CPython 3.12) | **204 PASS** |
| CLV planning-docs | 기존 HOLD 유지 (이번 변경과 무관) |

## 판정

```
EFFECT_GATEWAY_FUNCTIONAL   PASS
SINGLE_PROCESS_SERIALIZATION PASS  (journal flock, 비블로킹 → ErrGatewayBusy)
STALE_WRITER_REJECTION      PASS  (admission + effect 경계 2회 확인)
UNKNOWN_EFFECT_RECOVERY     PASS  (래치 유지, 자동 확정·자동 재시도 0)
CROSS_HOST_EFFECT_FENCING   NOT_VERIFIED  (단일 프로세스·단일 journal root)
CREDENTIAL_ISOLATION        NOT_VERIFIED  (Mac 단일 UID; VM 결과는 이 프로세스에 적용되지 않음)
```

## 잔여 TOCTOU

| 창 | 현재 | 남는 것 |
|---|---|---|
| admit → effect | effect 경계 재검증 | claim 조회와 provider 호출 사이 극소 구간 |
| `fenced()` 종료 → effect | 사용하지 않음(장기 트랜잭션 기각) | 위와 동일 |
| 효과 후 응답 유실 | `UNKNOWN_EFFECT` | provider truth는 reconcile로만 |
| 다른 호스트 | 없음 | S3 필요 |

## S3에 필요한 인터페이스 (구현은 시작하지 않음)

1. **writer↔gateway 전송**: 사설망(Tailscale) + 상호 인증. 현재 `Envelope`는 전송에 독립적이므로 그대로 실을 수 있다.
2. **세션 키 배포**: claim 시점에 호스트가 발급. `KeyRing` 구현만 추가하면 된다.
3. **claim 어댑터**: `SqliteClaimStore`를 읽는 `ClaimAuthority` 구현(기존 Python store 호출).
4. **2-host 경합 시나리오**: 동일 `TargetDigest`에 대해 Mac/Flip3가 각각 `Envelope` 제출 → 한쪽만 `APPLIED`.
5. **stale writer 재개 시험**: takeover 후 이전 generation의 봉투 재전송 → `ErrStaleGeneration`.

`CROSS_HOST_EFFECT_FENCING`은 위 4·5가 실제로 통과할 때만 선언한다.
