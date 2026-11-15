---
status: historical
owner: delivery
last_verified_commit: bf7a8b3ea50844b5312d1abfddf10ad982a16af7
---

# S4 — Linux Trusted Host 수령서 (부분 PASS)

동일한 격리 VM(`kc-isolation-20261111`)을 독립 Linux 환경으로 사용했다. OCI VM은
확보하지 않았고, 새 유료 자원도 만들지 않았다.

## 실행 환경

| 항목 | 값 |
|---|---|
| 게스트 | Ubuntu 26.04, kernel `7.0.0-34-generic`, aarch64 |
| Go | `go1.22.12 linux/arm64` (VM 내부 설치) |
| 실행 사용자 | `kcowner` (uid 1000, 비루트, sudo 없음) |
| 소스 | keycli `bf7a8b3` 트리 (host 기준 `tar` 전송) |

## 검증 항목

| # | 항목 | 방법 | 결과 |
|---|---|---|---|
| 1 | 실제 별도 Linux UID | `kcowner`/`kcpeer` (1000/1001) | **PASS** |
| 2 | 파일·디렉터리 소유권 격리 | 0700/0600 + peer 시도 | **PASS** (S1 영수증) |
| 3 | 최소 권한 실행 | 비루트 사용자로 Go 테스트 | **PASS** |
| 4 | Secret 접근 거부 | peer canary read | **DENIED** |
| 5 | journal 무단 쓰기 거부 | peer append/mkdir/chmod | **DENIED** |
| 6 | HostBridge 권한 경계 | `go test ./internal/hostbridge/` | **PASS** (Linux/arm64) |
| 7 | 실패 시 fail-closed | journal unsafe-storage·unknown-effect 테스트 포함 | **PASS** |
| 8 | stale writer 차단(단일 호스트) | `STALE_GENERATION`/`DUPLICATE` 테스트 | **PASS** |
| 9 | cross-host effect fencing | 별도 gateway 없음 | **NOT_VERIFIED** |

## 실행 로그 (비루트)

```
go version go1.22.12 linux/arm64
ok  github.com/camem2442/keycli/internal/hostbridge        0.029s
ok  github.com/camem2442/keycli/internal/delivery/journal  0.018s
ok  github.com/camem2442/keycli/internal/identity/ociwif   0.006s
```

## Gate

```
LINUX_ISOLATION_VERIFIED(host=kc-isolation-20261111) = PASS
TRUSTED_HOST_ELIGIBLE(host=kc-isolation-20261111)    = PASS (아래 조건 한정)
TRUSTED_HOST_ELIGIBLE(host=mac-local)                = NOT_VERIFIED
CROSS_HOST_EFFECT_FENCING                            = NOT_VERIFIED
OCI_VM_EFFECTS                                       = 0
```

`kc-isolation-20261111`은 이제 "별도 UID + 파일 격리 + 비루트 실행 + fail-closed"가
증명된 Linux 호스트다. 단 **provider 자격증명을 실제로 보유한 호스트가 아니므로**
Provider Live 인수의 근거는 아니다.

## Mac으로 전용하지 않는 이유

Mac 호스트는 여전히 단일 UID이고 peer 실행 권한이 없다. VM에서 통과한 항목을 Mac의
`TRUSTED_HOST`로 승격하지 않는다.

## 남은 것

1. cross-host **effect fencing**: 단일 gateway 구현(S2) + 2-host 경합(S3).
2. 실제 provider 자격증명 격리: 자격증명을 가진 호스트에서 동일 절차 재실행.
3. OCI/Linux 운영 인수는 별도 승인 시 같은 절차를 그대로 사용한다.

## 재현

```bash
limactl start kc-isolation-20261111
limactl shell kc-isolation-20261111 -- sudo -u kcowner bash -lc \
  'cd /srv/kc-src && PATH=/usr/local/go/bin:$PATH go test ./internal/hostbridge/... ./internal/delivery/journal/...'
```
