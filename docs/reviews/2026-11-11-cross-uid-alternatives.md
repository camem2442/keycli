---
status: historical
owner: delivery
---

# Cross-UID 검증 대안 조사 (Q2)

> **후속(2026-11-11):** 사용자가 신규 Lima VM을 허용했고, `kc-isolation-20261111`에서
> **실제 커널 UID로 PASS**했다([S1 영수증](2026-11-11-cross-uid-receipt.md)).
> 아래 분석은 "Mac 로컬에서 계정 생성 없이 가능한가"에 대한 답으로 남긴다.

질문: **별도 macOS 계정을 만들지 않고** 이 Mac에서 cross-UID 격리를 안전하게 검증할 수 있는가.

결론: **없다.** 커널이 강제하는 UID 경계를 만들려면 다른 UID로 실행해야 하고, 그 권한이
이 환경에 없다. 대안으로 보이는 것들은 실제로 경계가 아니다.

## 실측 환경

| 항목 | 관측 |
|---|---|
| 현재 사용자 | `k` (uid 501) |
| 로컬 계정 목록 | `daemon`, `k`, `nobody`, `root` — **일반 사용자 계정 1개뿐** |
| `sudo -n` | `sudo: a password is required` (passwordless 아님) |
| macOS | 27.2 (build 26B123) |
| `sandbox-exec` | 존재. `(allow default)`는 정상, `(deny default)`는 `execvp ... Operation not permitted` |
| Lima | `limactl` 설치, **`study-apps-ci` VM이 Running** (다른 프로젝트 소유, aarch64, 2CPU/2GiB) |

## 후보별 판정

| 후보 | 실제로 증명하는 것 | 판정 | 이유 |
|---|---|---|---|
| 기존 표준 계정 추가 | 진짜 커널 UID 경계 | **불가(이번 범위)** | 계정 생성은 관리자 인증 필요, 이번 작업에서 금지 |
| `sudo -u nobody` | 진짜 UID 경계 | **불가** | passwordless sudo 없음 |
| `sandbox-exec` 프로파일 | 실행 프로세스의 경로·동작 제한 | **부적합(경계 아님)** | 프로파일은 **실행자가 적용**해야 효력이 생긴다. 침해된 agent는 프로파일 없이 실행하면 그만이고, 같은 UID이므로 파일을 그대로 읽는다 |
| Keychain `-T` ACL | 항목별 앱 접근 제한(+ 사용자 프롬프트) | **부분** | Keychain 항목에만 적용. env/파일로 전달된 provider 토큰은 보호하지 못하고, 프롬프트 의존이라 자동 검증에 부적합 |
| `security create-keychain` 임시 키체인 | 격리된 테스트 저장소 | **도구로는 유효** | 임시 파일 키체인 생성/삭제는 실제 사용자 키체인을 건드리지 않는다(실측: create/delete exit 0). 다만 UID 경계 증명은 아니다 |
| Flip3 PRoot | 가상화된 UID | **부적합** | 커널 UID 격리가 아님(사용자 지적과 동일). writer 충돌·재시작 검증용으로만 |
| Lima VM 신규 생성 | 진짜 Linux UID 경계 | **실행됨 (PASS)** | 이미지 ~600MB 다운로드, 8GiB sparse. 관리자 권한 불필요 |
| 기존 `study-apps-ci` VM | Linux UID 경계 | **보류** | 다른 프로젝트 소유·운영 중. 사용은 소유자 승인 필요 |

## 실측으로 확인한 것

```
owner=k uid=501  file_owner=k  mode=600
same-UID read: READ_ALLOWED          ← 격리 없음(당연)
sandbox-exec (deny default): execvp denied, exit 71
sandbox-exec (allow default): hi
security create-keychain (temp): exit 0, delete-keychain: exit 0
```

즉 **같은 UID에서는 canary를 그대로 읽을 수 있고**, sandbox는 "우리가 자식 프로세스를
그 프로파일로 띄웠을 때만" 효력이 있다. 이는 agent 격리가 아니라 실행 래퍼다.

## 권장 순서 (비용·권한 기준)

| 순위 | 방법 | 필요 권한 | 비용 | 남는 risk |
|---|---|---|---|---|
| 1 | 사용자(관리자)가 표준 계정 1개 생성 → 별도 세션에서 canary 실행 | 관리자 승인 1회 | 0원 | 없음(진짜 UID 경계) |
| 2 | 신규 Lima VM (`vz`, Debian/Ubuntu) → VM 내부에서 서로 다른 UID | 관리자 불필요 | 이미지 다운로드·디스크 | 호스트↔VM 경계는 별도 확인 |
| 3 | 기존 `study-apps-ci` VM 재사용 | 소유자 승인 | 0원 | 다른 runner 상태 간섭 위험 |
| 4 | Flip3 (PRoot) | 없음 | 0원 | **UID 격리 아님** — writer 경합용으로만 |

## 즉시 실행 가능한 대체 검증(경계 아님, 보조)

- `scripts/isolation-canary.sh`는 그대로 유효하다. peer UID 실행 권한이 생기는 순간
  `--execute` 한 번으로 PASS/FAIL이 난다.
- `security` 임시 키체인으로 "항목 ACL" 동작을 실험할 수 있으나, 이는 앱 단위 보호이며
  UID 격리 증거로 승격하지 않는다.

## 판정

```
CROSS_UID_SECRET_ISOLATION = NOT_VERIFIED
이유 = 별도 UID 실행 권한 없음 (계정 생성·sudo 정책 변경은 범위 밖)
다음 조건 = 표준 계정 1개(관리자 승인) 또는 신규 Lima VM
```

계정 생성이나 sudo 정책 변경은 **자동으로 수행하지 않았고**, 앞으로도 이 작업 범위에서
수행하지 않는다. 사용자가 승인하면 `isolation-canary.sh --execute`로 즉시 마감된다.
