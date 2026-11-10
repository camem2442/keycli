---
status: historical
owner: delivery
last_verified_commit: 0039f39
---

# S3 — Cross-host gateway race (2026-11-11)

## 결과 요약

전송·프로토콜 계층은 완성됐고 **Mac 안에서 끝까지 실측**했다. 두 번째 호스트(Flip3)는
디바이스 접근 권한이 없어 **원격 실행을 시작하지 못했다.**

```
CROSS_HOST_TRANSPORT           PARTIAL   (tailnet HTTPS 왕복 PASS, peer 미실행)
CROSS_HOST_PROTOCOL_FENCING    NOT_VERIFIED (단일 호스트)
CLAIM_GENERATION_FENCING       PASS      (실제 claims DB 읽기)
SESSION_KEY_AUTHENTICATION     PASS      (발급 키·위조 키·세션 불일치)
DUPLICATE_MUTATION_SUPPRESSION PASS
STALE_WRITER_REJECTION         PASS
UNKNOWN_EFFECT_RECOVERY        PASS      (S2 + journal)
CROSS_HOST_EFFECT_FENCING      NOT_VERIFIED
CREDENTIAL_ISOLATION           NOT_VERIFIED
```

## 신규 구현 (최소)

| 파일 | 역할 |
|---|---|
| `internal/effectgateway/transport.go` | `POST /submit`, `GET /health`, 64 KiB 제한, unknown field 거부, 오류→HTTP 매핑 |
| `internal/effectgateway/keystore.go` | **Gateway가 발급**하는 세션 키(32B, TTL, 회수). writer 자기선언 불가 |
| `internal/effectgateway/claims.go` | 기존 SQLite claim 스키마를 `-readonly`로 조회 (`sqlite3` CLI). 스키마 재구현 없음 |
| `cmd/kc-gateway` | synthetic harness: ledger provider, allow-admission(로그), 키 발급 |
| `cmd/kc-effectclient` | 서명 제출 클라이언트. 키는 **환경변수**만, `-resolve`로 MagicDNS 없는 클라이언트 지원 |

기존 `Gateway.Submit`, `journal.Dispatch`, `hostbridge`는 수정하지 않았다.

## 실측 1 — 로컬 프로토콜 (gateway + client 두 프로세스)

| 시나리오 | 응답 | provider 호출 |
|---|---|---|
| 정상 제출 | `APPLIED` | 1 |
| 동일 mutation 재전송 | `DUPLICATE` | 0 (누적 1) |
| 세션 불일치 (claim session 변경) | `409 SESSION_MISMATCH` | 0 |
| owner 불일치 | `409 OWNER_MISMATCH` | 0 |
| generation 7 → claim 8 | `409 STALE_GENERATION` | 0 |
| 위조 키 | `401 UNAUTHENTICATED` | 0 |

ledger 최종 1줄.

## 실측 2 — tailnet HTTPS 경로 (portbridge)

```
portbridge preview 18090 --https-port 8458 --minutes 1
→ https://sj-mac.taile502be.ts.net:8458/
client --resolve sj-mac.taile502be.ts.net:8458:100.103.132.91
```

| 요청 | 결과 |
|---|---|
| 1차 | `200 APPLIED` (provider 1) |
| 2차 (동일 mutation) | `200 DUPLICATE` (provider 0) |
| 종료 후 Serve | 8458 라우트 잔여 없음 |

TLS는 공인 인증서로 기본 검증 통과. 8444는 다른 foreground 세션이 사용 중이어서 건드리지 않았다.

## 실측 3 — Flip3 (실행 못 함)

| 확인 | 결과 |
|---|---|
| tailnet 가시성 | `galaxy-z-flip3-5g` online |
| SSH 22 | **Connection refused** (sshd 미기동) |
| SSH 8022 (Termux) | 데몬 응답, **publickey/password 인증 실패** (등록된 키 없음) |
| `tailscale ssh` | 비활성 (22로 폴백 → refused) |
| `tailscale file cp` | `warning: not replying; trying anyway` (도착 미확인) |

즉 **네트워크는 닿지만 명령을 실행할 권한이 없다.** 기기 `authorized_keys` 수정과
tailnet ACL 변경은 이번 범위에서 금지되어 있으므로 진행하지 않았다.

## Flip3 실행 런북 (승인 후 그대로)

1. 바이너리 전달: `.tmp/kc-effectclient-linux-arm64` (4.8 MB, `GOOS=linux GOARCH=arm64`)
   ```sh
   tailscale file cp .tmp/kc-effectclient-linux-arm64 galaxy-z-flip3-5g:
   # 또는 scp -P 8022 …
   ```
2. 기기에서 실행:
   ```sh
   chmod +x kc-effectclient-linux-arm64
   export KC_EFFECT_KEY="$(cat session.key)"     # Mac에서 발급, 파일로만 전달
   ./kc-effectclient-linux-arm64 \
     -url https://sj-mac.taile502be.ts.net:8458/submit \
     -resolve sj-mac.taile502be.ts.net:8458:100.103.132.91 \
     -goal S3-RACE-20261111 -owner opencode-local -session ses-1 -generation 8 \
     -mutation op-flip-1 -target <T> -plan <P> -grant <G>
   ```
3. Mac에서 barrier: 두 호스트가 같은 `mutation_id`를 동시에 보내도록 시각을 맞춘다.
   `received_at` 차이가 임계값 이내일 때만 "동시"로 인정한다.

## 잔여 위험

- Mac gateway는 **단일 UID**에서 실행된다. 같은 UID 프로세스는 provider adapter를 직접 호출할 수 있다(S4 대상).
- claim 조회(`sqlite3 -readonly`)와 provider 호출 사이 TOCTOU는 좁혀졌지만 제거되지 않았다.
- provider는 idempotency key가 없어 exactly-once를 주장하지 않는다.
- 두 호스트가 각자 journal을 가지면 fence가 성립하지 않는다. S3는 단일 중앙 journal 전제다.

## 재현

```bash
# gateway
./.tmp/kc-gateway -addr 127.0.0.1:18090 -journal R/journal -claims R/claims.db \
  -ledger R/ledger.jsonl -key-out R/session.key \
  -goal S3-RACE-20261111 -owner opencode-local -session ses-1 -generation 8
# tailnet 노출
portbridge preview 18090 --https-port 8458 --minutes 1
# 클라이언트
KC_EFFECT_KEY=... ./.tmp/kc-effectclient -url https://<host>:8458/submit -resolve <host>:8458:<ip> ...
```
