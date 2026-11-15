---
status: historical
owner: delivery
last_verified_commit: 0039f390bfc659a1c1f8ea1f1f07c8375259127e
---

# S3 — Cross-host race 준비도 및 승인 대기 (2026-11-11)

## 판정: **BLOCKED_APPROVAL** — 원격 명령을 시작하지 않았다

S3는 "두 실제 호스트"가 전제다. 이 세션에는 **Flip3 참여에 대한 명시적 승인이 없다.**
지시된 중단 규칙에 따라 원격 실행 없이 Phase 0 조사와 테스트 계획까지만 완료한다.

```
CROSS_HOST_TRANSPORT          NOT_VERIFIED (BLOCKED_APPROVAL)
CROSS_HOST_PROTOCOL_FENCING   NOT_VERIFIED (BLOCKED_APPROVAL)
CLAIM_GENERATION_FENCING      PASS (단일 호스트, S2 실측)
SESSION_KEY_AUTHENTICATION    NOT_VERIFIED (KeyRing 구현체 없음 — fake만)
DUPLICATE_MUTATION_SUPPRESSION PASS (S2)
STALE_WRITER_REJECTION        PASS (S2, 단일 호스트)
UNKNOWN_EFFECT_RECOVERY       PASS (S2)
CROSS_HOST_EFFECT_FENCING     NOT_VERIFIED
CREDENTIAL_ISOLATION          NOT_VERIFIED
```

## Phase 0 — 조사 결과

| 항목 | 관측 |
|---|---|
| keycli HEAD / tree | `0039f39` / `03e50564` (기준과 일치) |
| workspace-infra | `8ee9558` (기준과 일치) |
| 공유 checkout | keycli `0e45e9c` clean, infra `99657d1` WIP 11건 유지 |
| 실행 중 gateway 프로세스 | **없음** |
| Gateway 계약 | `effect-gateway-v1.md`: 서명 → 만료 → claim → admission → intent → 경계 재검증 → provider |
| Journal 보장 | `Dispatch` 중복/target latch, `flock`(비블로킹), `UNKNOWN_EFFECT` 래치 |
| ClaimAuthority 구현체 | **없음** (테스트 fake만) |
| KeyRing 구현체 | **없음** (테스트 fake만) |
| Provider 구현체 | **없음** (테스트 fake만) |
| 전송 계층 | **없음** (`net.Listen`/`http.Server` 0건) |
| Tailscale | tailnet에 `galaxy-z-flip3-5g` (100.111.62.94) **active** 관측 |
| SSH config Host 항목 | 없음 (`~/.ssh/config`에 Flip3 별칭 미정의) |
| Flip3 승인 문서 | **없음** (infra `docs/planning/flip3-*.md`는 워크플로 계획이며 이번 사용 승인이 아님) |
| Lima VM | `kc-isolation-20261111` Stopped 유지 (시작하지 않음) |

**결론: 전송·세션 키·claim 어댑터가 모두 미구현이며, 원격 호스트 사용 승인도 없다.**
따라서 S3 게이트를 진행할 수 없다.

## 승인 후 착수할 최소 구현 (아직 작성하지 않음)

| 파일 | 내용 | 재사용 |
|---|---|---|
| `internal/effectgateway/transport.go` | `http.Server` (기본 `127.0.0.1:0`), 요청 본문 digest 검증, `MaxBytesReader`, 타임아웃, tailnet 출처는 참고만 | `Envelope` 그대로 |
| `internal/effectgateway/keystore.go` | 세션 키 발급/회수. **키는 Gateway가 발급**하고 writer는 받기만 한다. claim generation·session에 결속, TTL, 회수 목록 | 기존 HMAC |
| `internal/effectgateway/claimstore.go` | 기존 `SqliteClaimStore` DB를 **읽기 전용**으로 조회 (`mode=ro`). 실제 `claims.db`가 아니라 테스트용 임시 DB | 기존 스키마 |
| `internal/effectgateway/ledger.go` | synthetic effect ledger (append-only JSONL + sha256 chain) | 신규(테스트 전용) |
| `cmd/kc-gateway/` (테스트 하네스) | Gateway 기동, `/submit`, `/health` | 신규 |

금지 사항(설계에 반영):

- 공개 인터넷 노출·Funnel 금지. Tailscale Serve 또는 loopback + tailnet only.
- 출발지 IP만으로 인증하지 않는다(서명이 유일한 인증).
- 클라이언트가 서명 키를 임의 선언하는 구조 금지 → 키는 Gateway 발급.
- SQLite 쓰기 트랜잭션을 provider 호출 동안 유지하지 않는다.
- 두 호스트에 journal을 나누지 않는다(중앙 journal root 하나).

## Two-host 테스트 계획 (승인 후)

### 구성

| 역할 | 호스트 | 실행 |
|---|---|---|
| A | Mac | Gateway + fake provider + 중앙 journal |
| B | Flip3 | writer 클라이언트 (`Envelope` 서명 후 전송) |
| 제어 | Mac | barrier(양쪽 ready 신호 후 동시 제출), ledger 수집 |

### 필수 시나리오

| # | 시나리오 | 기대 |
|---|---|---|
| 1 | 동일 mutation ID 동시 제출 | effect 1회, 한쪽 `APPLIED`, 다른 쪽 `DUPLICATE` |
| 2 | 서로 다른 owner의 generation 경합 | 최신 generation만 `APPLIED` |
| 3 | takeover 후 stale generation 재전송 | `ErrStaleGeneration`, effect 0 |
| 4 | 위조 Envelope (서명/필드 변조) | `ErrUnauthenticated`, effect 0 |
| 5 | 세션 만료·키 회수 후 제출 | `ErrUnauthenticated` |
| 6 | claim 재검증 직후 pause injection | provider 호출 0, `OBSERVED_NO_EFFECT` |
| 7 | Gateway crash & restart | 재시작 후 redispatch 0, `DUPLICATE` |
| 8 | 응답 유실 | `UNKNOWN_EFFECT`, 재시도 0 |
| 9 | 네트워크 단절·복구 | 단절 중 실패는 fail-closed, 복구 후 중복 0 |
| 10 | Gateway 우회 시도 | 우회 경로 존재 여부 기록(별도 판정) |

### Barrier 증명

- 양쪽이 `barrier_id`를 먼저 전송 → Gateway가 두 도착 시각을 기록
- 두 요청의 `received_at` 차이가 사전 임계(예: 50ms) 이내일 때만 "동시"로 인정
- ledger에는 `{mutation_id, generation, owner, session, received_at, provider_calls, ledger_seq, ledger_hash}`

### 실패 주입 원칙

- 실제 시스템 프로세스 종료 금지. **테스트가 띄운 자식만** 정확한 PID로 종료.
- PID 0/1, 프로세스 그룹, 기존 runner, `study-apps-ci` VM은 대상 아님.
- 네트워크 단절은 테스트 프로세스의 loopback 포트/프록시 레벨에서만.

## 승인 요청 (이것만 확인되면 착수)

1. **Flip3 사용 승인**: S3 기간 동안 writer 역할로 접속·실행해도 되는지.
2. **접속 경로**: tailnet 주소 `100.111.62.94` 사용 여부, 사용자명, SSH/터미널 방식(Termux?).
3. **설치 허용 범위**: Flip3에 Go 툴체인 또는 정적 바이너리 1개를 두어도 되는지.
4. **tailnet 정책**: ACL 변경 없이 기존 연결만 사용함을 확인.

승인 후 예상 작업량: 코드 4파일 + 하네스 1개 + 테스트 10종, Mac↔Flip3 왕복 1회.
`CROSS_HOST_EFFECT_FENCING`은 1·2·3·9·10이 실제 로그로 통과할 때만 선언한다.

---

## portbridge 통합 결정 (2026-11-11, 사용자 제안)

`camem2442/portbridge`를 **S3 전송 계층**으로 채택한다. Gateway 앞단에서 tailnet 전용
HTTPS 경로만 제공하고, 인증은 기존 HMAC Envelope가 그대로 담당한다.

### 사전 점검 (읽기 전용, Mac에서 실행)

```
PASS Tailscale backend: Running
PASS This Mac on tailnet: sj-mac.taile502be.ts.net
PASS HTTPS certificates: sj-mac.taile502be.ts.net
PASS Local SSH 22: reachable
PASS Existing Serve ports: 8443
PASS Free tailnet HTTPS port: 8444 (점검 시점)
```

### 전송 왕복 실측 (Flip3 없이, 로컬 서비스 대상)

| 단계 | 결과 |
|---|---|
| loopback echo 서버 → `portbridge preview 18083 --https-port 8457 --minutes 1` | 경로 생성 |
| `curl --resolve sj-mac…:8457:100.103.132.91 https://sj-mac…:8457/` | **HTTP 200**, marker 일치 |
| TLS 검증 | 기본 검증 통과(공인 인증서) |
| 종료 후 `tailscale serve status` | 8457 라우트 **잔여 없음** |

### 확인된 제약 (S3 설계에 반영)

1. **Serve는 loopback에 바인딩하지 않는다.** Mac에서 자기 자신을 호출할 때도 tailnet IP를 써야 한다(127.0.0.1 접속 불가).
2. **시스템 DNS로 tailnet FQDN이 해석되지 않는다.** 이 Mac의 resolver는 `8.8.8.8`이고, `100.100.100.100`(MagicDNS)으로는 해석된다. 클라이언트가 MagicDNS를 쓰지 않으면 `--resolve`/Host 지정이 필요하다.
3. **8444는 이미 다른 foreground 세션이 점유**(→ `127.0.0.1:8899`). 건드리지 않고 빈 포트를 명시 지정한다.
4. URL 출력 후 실제 라우트 반영까지 약 2–3초가 걸린다. 테스트는 readiness poll을 둔다.
5. `preview`는 대상이 **loopback 전용**일 때만 동작한다 → Gateway 바인딩과 일치.

### 역할 분리

| 계층 | 담당 |
|---|---|
| tailnet 노출·TLS·경로 수명 | portbridge (운영 도구, 라이브러리 의존 아님) |
| 요청 인증(HMAC)·세션 키 | KeyCLI gateway |
| claim·generation fencing | 기존 ClaimStore + gateway |
| 효과 실행 | gateway의 synthetic provider |

portbridge는 **인증 수단이 아니다**. tailnet 접근 가능성만 제공하므로, 출발지 IP를 인증으로
쓰지 않는다는 S3 원칙이 그대로 유지된다.

### 남은 승인 (변경 없음)

- Flip3를 writer로 사용해도 되는지 (원격 실행 승인)
- Flip3에서 서명 클라이언트를 어떻게 실행할지: (a) Termux Python 스크립트, (b) Mac에서 cross-compile한 정적 Go 바이너리(linux/arm64)
