---
status: historical
owner: delivery
last_verified_commit: 02d6a75
---

# S3 — Cross-host gateway race (Mac ↔ Galaxy Z Flip3)

두 실제 호스트에서 프로토콜 경합과 복구를 실측했다. provider mutation은 0회이며
synthetic ledger만 사용했다.

## 호스트

| 역할 | 호스트 | 접속 |
|---|---|---|
| Gateway | Mac (`sj-mac.taile502be.ts.net`, 100.103.132.91) | loopback `127.0.0.1:18091` |
| Writer | Galaxy Z Flip3 (Termux, Linux aarch64) | tailnet HTTPS `:8459/8460/8461` |
| 전송 | portbridge (`preview`) | tailnet 전용, Funnel 없음 |
| 인증 | Gateway 발급 세션 키(HMAC-SHA256) | writer 자기선언 불가 |

## 실행 환경

| 항목 | 값 |
|---|---|
| Gateway | `cmd/kc-gateway` (Go 1.22.12, 커밋 `02d6a75` 기준) |
| Claim | 테스트용 SQLite (`S3-RACE-20261111`, gen 8→9) — 실제 `claims.db` 미사용 |
| Journal | 단일 중앙 root (`.tmp/s3y/journal`) |
| Provider | ledger provider (append-only, sha256 chain) |
| Flip3 클라이언트 | `scripts/s3-effect-client.py` (Termux python3) |

### Go 정적 바이너리 대신 Python을 쓴 이유

`GOOS=linux GOARCH=arm64` 바이너리는 Termux에서 실행되지 않았다.

```
e_type: 2 (ET_EXEC)                         → -buildmode=pie 로 재빌드
TLS segment underaligned: 8, needs 64 (Bionic) → Go 1.22/1.27 모두 동일
```

Bionic TLS 정렬 요구 때문이며, 제품 코드 문제가 아니다. Termux에 python3가 이미 있어
동일한 서명 규약(HMAC + length-prefixed payload)을 구현한 스크립트로 대체했다.
프로토콜 증명에는 영향이 없다.

## 결과

| # | 시나리오 | 결과 |
|---|---|---|
| 1 | Flip3 단일 제출 | `200 APPLIED` (provider 1) |
| 2 | 동일 mutation 재전송 | `200 DUPLICATE` (provider 0) |
| 3 | **동시 제출 (barrier)** | 두 요청 수신 간격 **60ms**, `APPLIED` 1 + `429 GATEWAY_BUSY` 1, provider 1 |
| 4 | 세션 불일치 (claim session 변경) | `409 SESSION_MISMATCH` |
| 5 | takeover 후 stale generation | `409 STALE_GENERATION` |
| 6 | 키 회전 후 구 키 사용 | `401 UNAUTHENTICATED` |
| 7 | 새 키 배포 후 재시도 | `200 APPLIED` |
| 8 | 전송 단절 (preview 중지) | `ConnectionRefused` — 효과 0 |
| 9 | 전송 복구 (새 포트) | `200 APPLIED` |
| 10 | 복구 후 중복 | `200 DUPLICATE` (0) |
| 11 | Gateway 재시작 후 이전 mutation 재전송 | `200 DUPLICATE` (0), journal 파일 유지 |
| 12 | ledger 총계 | **9줄** (효과 1건당 1줄, 거부·중복 0줄) |

### Barrier 증명

```
clock_offset(mid) = +0.235s, 보정 후 두 요청 도착:
17:39:31.897961Z  (Mac)     → APPLIED
17:39:31.958257Z  (Flip3)   → GATEWAY_BUSY
```

60ms 창 안에서 두 요청이 실제로 겹쳤고, journal `flock`(비블로킹)이 한쪽만 통과시켰다.
패자는 큐 대기 없이 즉시 거부됐다(fail-closed).

## 판정

```
CROSS_HOST_TRANSPORT            PASS   (Flip3→Mac tailnet HTTPS, TLS 검증 + SNI 고정)
CROSS_HOST_PROTOCOL_FENCING     PASS   (60ms 동시성, 효과 1회)
CLAIM_GENERATION_FENCING        PASS   (stale 409, 세션 409)
SESSION_KEY_AUTHENTICATION      PASS   (회전 후 401, 새 키 200)
DUPLICATE_MUTATION_SUPPRESSION  PASS   (중복 0회)
STALE_WRITER_REJECTION          PASS
UNKNOWN_EFFECT_RECOVERY         PASS   (S2 유닛 + journal 래치, cross-host 재실행은 안 함)
CROSS_HOST_EFFECT_FENCING       PASS (프로토콜/합성 한정)
PRODUCT_EFFECT_FENCING          NOT_VERIFIED (same-UID 우회 + 자격증명 격리 미증명)
CREDENTIAL_ISOLATION            NOT_VERIFIED
```

제품 전체 effect fencing은 선언하지 않는다. 이 결과는 **단일 gateway + 단일 중앙 journal +
합성 provider** 구성에서의 프로토콜 증명이다.

## 잔여 위험

| 위험 | 상태 |
|---|---|
| 같은 UID 프로세스가 gateway/provider adapter를 직접 호출 | 남음 (S4) |
| claim 조회와 provider 호출 사이 TOCTOU | 좁힘, 미제거 |
| provider idempotency/fencing token 부재 | exactly-once 미보장 |
| 두 호스트가 각자 journal을 쓰는 구성 | 이번 테스트는 단일 중앙 journal 전제 |

## 재현

```bash
# Mac
./.tmp/kc-gateway -addr 127.0.0.1:18091 -journal R/journal -claims R/claims.db \
  -ledger R/ledger.jsonl -key-out R/session.key \
  -goal S3-RACE-20261111 -owner opencode-local -session ses-1 -generation 8
portbridge preview 18091 --https-port 8459 --minutes 10
# Flip3
KC_EFFECT_KEY=$(cat ~/s3/session.key) python3 ~/s3/client.py --url https://<host>:8459/submit \
  --resolve <host>:8459:<ip> --goal S3-RACE-20261111 --owner opencode-local --session ses-1 \
  --generation 8 --mutation op-1 --target <T> --plan <P> --grant <G>
```
