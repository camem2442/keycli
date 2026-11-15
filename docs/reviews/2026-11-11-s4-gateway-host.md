---
status: historical
owner: delivery
last_verified_commit: cf972bb
---

# S4 — 격리 Gateway 호스트 인수 (2026-11-11)

기존 Lima VM을 Gateway 전용 Linux 실행 환경으로 구성하고, synthetic provider로
격리·복구·우회 가능성을 실측했다.

기준: keycli `cf972bb` (tree `be33d2a6`), workspace-infra `8ee9558`.
VM: `kc-isolation-20261111` (Ubuntu 26.04 aarch64) — 작업 후 **Stopped 복원**.

## S4-A — Trust boundary audit

| 계층 | 실제 권한 | 판정 근거 |
|---|---|---|
| Guest 비특권 UID (`kcpeer` 1001) | 서비스 파일 접근 불가, sudo 불가, 프로세스 신호 불가 | S4-E |
| Guest 서비스 UID (`kcgw` 999) | journal·credential·claims 소유 | systemd `User=kcgw` |
| Guest root | 전부 접근 가능 | `sudo -n id -un` → root |
| Mac VM owner (= Mac 에이전트 UID) | **guest root 동등** (`limactl shell … sudo`), 디스크 이미지 직접 읽기 | 아래 우회 경로 |
| Mac 관리자 | 동일 + Lima 설치/설정 변경 | 범위 밖 |
| 원격 writer (Flip3) | HMAC 서명 요청만 가능 | S4-C |

관측:

```
~/.lima/kc-isolation-20261111        drwx------ k
~/.lima/kc-isolation-20261111/disk   -rw-r--r-- 8589934592 (8 GiB, Mac 사용자 읽기 가능)
guest mounts                          /Users/k (virtiofs, ro)
guest sudoers                         k ALL=(ALL) NOPASSWD:ALL
```

## S4-B — Gateway service

| 항목 | 값 |
|---|---|
| 단위 | `/etc/systemd/system/kc-gateway.service` (enabled) |
| 실행 UID | `kcgw` (999), 비루트 |
| 수신 | `127.0.0.1:18091` (loopback only) |
| 상태 | `StateDirectory=kcgw`, `StateDirectoryMode=0700`, `UMask=0077` |
| 하드닝 | `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `PrivateDevices=yes`, `ProtectKernel*=yes`, `RestrictSUIDSGID=yes`, `LockPersonality=yes`, `CapabilityBoundingSet=`(빈 값), `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX` |
| 데이터 | `/var/lib/kcgw/{journal (0700), cred (0700), claims.db (0600), ledger.jsonl, session.key}` 모두 kcgw 소유 |
| Credential | `cred/provider.token` 0600 kcgw, **synthetic 값만** |

발견: 임시(fault) 단위를 `StateDirectoryMode` 없이 띄웠을 때 `/var/lib/kcgw`가 0755로
완화되어 peer가 `claims.db`를 읽을 수 있었다. 즉시 `0700`/`0600`으로 복구하고 재시험해
차단을 확인했다. 임시 단위에도 모드를 명시해야 한다.

## S4-C — Remote writer regression (Mac + Flip3 → VM)

| 시나리오 | 결과 |
|---|---|
| Mac → VM (tailnet relay 경유) | `200 APPLIED` |
| Flip3 → VM 단일 | `200 APPLIED` |
| Flip3 동일 mutation 재전송 | `200 DUPLICATE` (0 calls) |
| Barrier (Mac+Flip3 동시, 194ms 간격) | `APPLIED` 1 + `DUPLICATE` 1, ledger +1 |
| Relay 변조 (target digest 변경, 서명 유지) | **`401 UNAUTHENTICATED`** |

전송 구조: `Flip3/Mac → tailnet HTTPS(8462) → Mac portbridge relay → Lima port-forward → VM gateway`.
Mac relay는 신뢰하지 않으며, 본문 변조는 HMAC에서 거부된다. TLS 종단은 Mac relay에서
이루어지고, VM 구간은 loopback 평문이지만 tailnet 밖으로 나가지 않는다.

## S4-D — Crash / recovery

| 단계 | 관측 |
|---|---|
| provider 적용 후 응답 유실 (`-fail-after-apply`) | `UNKNOWN_EFFECT`, provider 1회, ledger 1 |
| 단위 재시작 후 동일 mutation | `DUPLICATE`, provider **0**, ledger 1 유지 |
| 명시적 `-reconcile` (ledger proof) | `OBSERVED_APPLIED` |
| 자동 redispatch | **0** |

## S4-E — Isolation negative tests

| 대상 | 결과 |
|---|---|
| `kcpeer` → `cred/provider.token` | Permission denied |
| `kcpeer` → `journal/` | Permission denied |
| `kcpeer` → `claims.db` (0600 복구 후) | Permission denied |
| `kcpeer` → `session.key` | Permission denied |
| `kcpeer` journal 쓰기 | Permission denied |
| `kcpeer` sudo | 거부 |
| `kcpeer` → gateway 프로세스 `kill -0` | Operation not permitted |
| 로그에 synthetic 토큰 | 0건 |
| 서비스 프로세스 UID | `kcgw` |

## Q1 — Mac 호스트 소유자의 우회 경로 (전수)

| 경로 | 가능 여부 | 결과 |
|---|---|---|
| `limactl shell <vm> -- sudo cat /var/lib/kcgw/cred/provider.token` | **가능** | 실제로 synthetic 토큰이 읽힘 |
| `~/.lima/<vm>/disk` 직접 읽기 (8 GiB, 0644) | **가능** | 오프라인으로 guest FS·credential 추출 가능 |
| `~/.lima/<vm>/lima.yaml` 수정 후 재기동 (mount 추가) | 가능 | guest에 host 경로 주입 |
| `~/.lima/<vm>/ssh/key` 로 guest 로그인 | 가능 | guest user `k` = passwordless sudo |
| 호스트 홈 마운트(`/Users/k`, ro) 통해 guest→host 읽기 | 가능(읽기) | host 파일 노출 |
| guest → host 쓰기 | 불가 | 마운트 ro |
| 네트워크 경유(포트포워드) 우회 | 제한적 | loopback 포워드만, 게이트웨이 인증 유지 |

**결론:** `MAC_HOST_OWNER_ISOLATION = NOT_VERIFIED`. Mac 사용자와 같은 UID로 도는
에이전트는 `limactl`/디스크 이미지를 통해 guest root와 동등하다. Lima는 **개발·검증용
Gateway**로는 적합하지만, 적대적 same-UID 프로세스로부터 실제 provider 토큰을 보호하는
경계는 아니다.

## Q2 — OCI 무료 VM vs Lima

| 항목 | Lima (현재) | OCI Always Free VM |
|---|---|---|
| Guest UID 격리 | PASS | PASS (동일 커널 메커니즘) |
| Mac same-UID 에이전트의 우회 | **가능** (limactl, 디스크) | **차단** — 로컬 디스크 접근 경로 없음 |
| 자격증명 보관 위치 | Mac 디스크 위 이미지 | 원격 블록 볼륨 |
| 테넌시 관리자 우회 | 해당 없음 | 가능(볼륨 분리/콘솔) — **다른 신뢰 경계**, Mac 에이전트와 분리 |
| 비용 | 0원 | Always Free 범위 내 0원 (A1/E2 재고 의존) |
| 확보 난이도 | 즉시 | Osaka A1 재고 부족 이력 |
| 운영 부담 | Mac 재부팅 시 중지 | 상시 기동, 패치·모니터링 필요 |
| 네트워크 | Lima port-forward + tailnet | tailnet 직접 가입 |
| 적합 용도 | 개발·검증, 프로토콜 회귀 | **운영 인수 후보** |

판단: OCI는 "Mac 에이전트로부터의 격리"라는 핵심 위협을 실제로 제거한다. 다만
테넌시 관리자(사용자 본인)는 여전히 상위 권한이며, 이는 별도 신뢰 계층으로 문서화해야
한다. 무료 재고가 없으면 S4 판정을 OCI로 승격하지 않고 `BLOCKED(환경)`으로 남긴다.

## 판정

```
LINUX_GUEST_UID_ISOLATION      PASS
GATEWAY_SERVICE_ISOLATION      PASS
GUEST_CREDENTIAL_ISOLATION     PASS
MAC_HOST_OWNER_ISOLATION       NOT_VERIFIED
CROSS_HOST_PROTOCOL_FENCING    PASS  (VM gateway 경유 회귀)
CROSS_HOST_EFFECT_FENCING_SYNTHETIC PASS
GATEWAY_BYPASS_RESISTANCE      PARTIAL (타 UID 차단 / host owner 우회 가능)
JOURNAL_CRASH_RECOVERY         PASS
UNKNOWN_EFFECT_RECOVERY        PASS
PRODUCT_CREDENTIAL_ISOLATION   NOT_VERIFIED
TRUSTED_HOST                   SOURCE_PARTIAL
PROVIDER_LIVE                  NOT_VERIFIED
```

## S5 진입 조건

1. `MAC_HOST_OWNER_ISOLATION`을 닫으려면 Gateway를 **Mac 밖**(OCI/독립 호스트)으로 옮기고
   자격증명을 그 호스트에만 둔다.
2. 그 호스트에서 S4-C/D/E를 그대로 재실행해 증거를 새 HEAD에 결속한다.
3. 그 전에는 운영용 v0.10.0을 선언하지 않는다. 개발용(provider 비활성) RC만 검토 가능하다.
