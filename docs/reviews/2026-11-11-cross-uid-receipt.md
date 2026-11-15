---
status: historical
owner: delivery
last_verified_commit: bf7a8b3ea50844b5312d1abfddf10ad982a16af7
---

# S1 — Cross-UID isolation 영수증 (PASS)

승인된 신규 Lima VM에서 **실제 커널 UID**로 검증했다. Mac 로컬에서는 peer UID 실행
권한이 없어 불가능했던 항목이다.

## 실행 환경

| 항목 | 값 |
|---|---|
| VM | `kc-isolation-20261111` (Lima 2.2.1, `vz`, aarch64) |
| 게스트 | Ubuntu 26.04 (resolute), kernel `7.0.0-34-generic` |
| 자원 | 1 CPU / 1 GiB / 8 GiB (sparse) |
| 계정 | `kcowner` uid **1000**, `kcpeer` uid **1001** (VM 내부에서 생성) |
| 호스트 | macOS 27.2, peer UID 실행 권한 없음(`sudo -n` 불가) |

## 실험 1 — POSIX canary (두 실제 사용자)

| 검사 | 결과 |
|---|---|
| `P1` 소유자(`kcowner`) canary 읽기 | **PASS** |
| `N1` peer(`kcpeer`) canary 읽기 | **DENIED** (`Permission denied`) |
| `N2` peer 디렉터리 목록 | **DENIED** |
| `N3` peer journal 디렉터리 쓰기 | **DENIED** |
| `N4` 심볼릭 링크(`/tmp/kc-canary-link` → canary) 우회 | **DENIED** |
| `N5` peer 출력에 canary 문자열 부재 | **PASS** |
| peer 프로세스 실제 UID | **1001** (`kcpeer`) |

권한: 디렉터리 `0700 kcowner`, 파일 `0600 kcowner`.

## 실험 2 — 표준 하네스 (`scripts/isolation-canary.sh --execute`)

```
cross_uid_read               PASS
cross_uid_directory_listing  PASS
canary_absent_from_output    PASS
same_uid_boundary            APPLICATION_LEVEL_ONLY
status=PASS scope=cross-uid-read-only
```

하네스는 실제 사용자 Keychain·`~/.kc`·provider token을 읽지 않고 synthetic canary만
사용한다. VM 내부에서는 `sudo -n -u nobody`가 성립하므로 `--execute`가 그대로 동작했다.

## 실험 3 — journal root 무단 접근

| 검사 | 결과 |
|---|---|
| `kcowner` journal root(`0700`) append | **PASS** |
| `kcpeer` append | **DENIED** |
| `kcpeer` mkdir | **DENIED** |
| `kcpeer` read | **DENIED** |
| `kcpeer` chmod | **DENIED** |

## Gate

```
CROSS_UID_SECRET_ISOLATION = PASS  (scope: kc-isolation-20261111)
CROSS_UID (mac-local)      = NOT_APPLICABLE (peer UID 권한 없음)
```

## 정직한 범위

- 이것은 **VM 안에서** 별도 UID가 서로의 파일에 접근하지 못함을 보인 것이다.
- Mac 호스트(에이전트가 실제로 도는 곳)는 여전히 단일 UID이며, 그 환경의 격리는
  `NOT_VERIFIED`다. VM 결과를 Mac으로 전용(轉用)하지 않는다.
- provider 자격증명은 사용하지 않았다(mutation 0, GET 0).

## 재현

```bash
limactl start kc-isolation-20261111
limactl copy scripts/isolation-canary.sh kc-isolation-20261111:/tmp/
limactl shell kc-isolation-20261111 -- sudo bash /tmp/isolation-canary.sh --execute
```
