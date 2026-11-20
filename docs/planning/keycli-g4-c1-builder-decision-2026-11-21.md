---
status: BLOCKED
owner: keycli
date: 2026-11-21
keycli_main: 5650d0a92cfe4f15cf2f086e20bb7cfbf7e0553f
scope: G4-C1 결정 패키지; 워크플로·러너·설정 변경 없음, 릴리스 권한 없음
---

# G4-C1 — 신뢰할 수 있는 ephemeral builder 결정 패키지

이 문서는 G4의 선행 조건인 **C1(격리된 일회성 Linux 빌더 선정·등록·철회)** 을
결정 가능한 형태로 고정한다. 제품 코드·워크플로·러너·secret·태그를 바꾸지 않으며,
문서 자체는 릴리스 승인도 운영 권한도 아니다.

## 1. 왜 C1이 다음 골인가

[2026-11-19 정본](keycli-current-roadmap-2026-11-16.md)의 남은 작업 중 릴리스 층을
막는 유일한 선행 조건이다. G3은 Mac native 실측이 필요하고, G5/G6은 C4 이후다.
반면 C1은 코드가 아니라 **호스트·비용·철회 절차의 결정**이므로 문서로 먼저 닫아야
나머지가 진행된다.

## 2. 현재 차단 사실 (2026-11-21 조회)

| 항목 | 사실 | 근거 |
|---|---|---|
| 릴리스 차단 | `release.yml` classify Job이 `if: false` | `.github/workflows/release.yml:50`, 소스 `9691e99`(#75) |
| 등록 러너 | 저장소 등록 self-hosted 러너 **0개** | `gh api repos/camem2442/keycli/actions/runners` → `total_count: 0` |
| 공개 CI | `test.yml`의 `isolated-source` Job도 `&& false`로 영구 차단 | `.github/workflows/test.yml` |
| prerelease | `v0.10.0-rc.1000`·`rc.1001` = `80d1408`, Linux ARM64 자산만 | tag 조회 |
| 정식 릴리스 | v0.8.0(`ad30607`)이 Latest, v0.10 정식 태그 없음 | tag/Release 조회 |
| G4 게이트 | nonroot·native Linux VM에서만 실행, ARM64/AMD64 각 1회 | `scripts/g4-free-release-gate.sh` |

즉 "릴리스 워크플로가 실행되지 않는" 이유는 코드 결함이 아니라 **승인된 빌더가 없기 때문**이다.

## 3. C1의 완료 정의

C1은 다음 세 산출물이 모두 갖춰진 상태다.

| 산출물 | 내용 |
|---|---|
| C1a 선정 결정서 | 호스트 종류·격리 등급·비용 상한·철회 책임자 |
| C1b 등록·첫 실행 영수증 | ARM64 native `g4-free-release-gate.sh <sha> arm64` 원본 로그·run ID |
| C1c 두 번째 아키텍처·teardown | AMD64 native 실행 영수증 + 러너 삭제/VM 폐기 영수증 |

C1은 릴리스가 아니다. `classify if: false` 해제(C2)는 **별도 PR**이며, C1 산출물과
서로 다른 검토 단위다.

## 4. PASS 조건 (빌더가 갖춰야 하는 속성)

1. **exact source**: reviewed `head`/`base` SHA에서 시작하고, 실행 전후에 `git status --porcelain`이 비어 있다.
2. **non-root·무자격증명**: `id -u != 0`, `KC_PROVIDER_EFFECTS`·Cloudflare/GitHub/OpenAI 토큰 부재.
3. **offline module lock**: `GOPROXY=off`, `GOFLAGS=-mod=readonly`, `GOTOOLCHAIN=local`, `GOWORK=off`.
4. **native architecture**: ARM64와 AMD64를 각각 실제 실행. 교차 실행·에뮬레이션은 native PASS로 승격하지 않는다.
5. **일회성**: 등록 → 실행 → 삭제가 한 사이클이며 상시 대기 러너를 공개 publisher로 쓰지 않는다.
6. **무료 폴백 금지**: GitHub-hosted 러너로 자동 대체하지 않는다.
7. **비용·기간 상한**: 호스트/네트워크/전력은 GitHub minutes 밖 비용이므로 사전 상한을 정한다.
8. **영수증 보존**: run ID, source SHA, manifest SHA, runner ID/삭제 시각, 원본 로그.

## 5. 옵션 비교

| 옵션 | 장점 | 제약 | 현 판정 |
|---|---|---|---|
| Mac 위 Linux VM (Lima/UTM) | 추가 하드웨어 없음, 기존 `utm-ci` 경로 재사용 가능 | Mac 의존, 중첩 가상화 성능, teardown 자동화 필요 | **검토 대상** |
| 전용 Linux 호스트 | 격리·성능·재현성 | 비용·유지보수, 상시 러너 금지 규칙과 충돌하지 않게 1회성 운용 필요 | **검토 대상** |
| Flip3 | 이미 존재 | 상시 대기 러너를 공개 릴리스 publisher로 쓰는 것은 금지, PRoot은 격리 경계 아님 | **부적합** |
| GitHub-hosted | 운영 부담 없음 | 무료 폴백 금지, 유료 minutes | **금지** |
| 외부 CI 제공자 | 격리·확장성 | 계정·비용·새 공급망 신뢰 필요 | 보류 |

## 6. C1a/C1b/C1c 실행 순서

1. **C1a**: owner가 옵션을 고르고 비용 상한·철회 책임자를 기록한다(이 문서에 append).
2. **C1b**: 선택한 호스트에서 `scripts/g4-free-release-gate.sh <reviewed-sha> arm64`를 1회 실행하고 원본 로그를 보존한다.
3. **C1c**: AMD64 native 실행 1회 + 러너/VM teardown 영수증. 두 실행의 source SHA가 같아야 한다.
4. 이후 C2(차단 해제 PR)를 별도로 연다.

## 7. 오늘 변경과의 연결 (#90)

PR #90은 `verify.sh`의 release smoke가 **호스트 아키텍처에 맞는 바이너리**를 실행하도록 바꿨다.
`g4-free-release-gate.sh:51`의 "ARM64에서는 전체 `--suite all`을 건너뛴다" 우회는 그 이유가
"amd64 바이너리 실행"이었으므로, #90이 병합되면 기술적 필수조건이 아니게 된다.

다만 `scripts/tests/test-g4-free-release-contract.py`가 그 우회 토큰
(`if [[ "$arch" == amd64 ]]; then`)을 계약으로 고정하고 있으므로, 게이트 변경은
**별도 PR + 재검증**이 필요하다. #90 병합 전에는 현재 동작을 유지한다.

## 8. 금지 (HOLD 사유)

- C1 산출물 없이 `classify if: false`를 해제하는 것
- standing Flip3를 공개 저장소 릴리스 publisher로 사용하는 것
- hosted 러너 자동 폴백, 합성 `admitted_isolated_vm=true`
- skipped/queued/cancelled 실행을 PASS로 승격
- 태그·Release 생성, `scripts/install.sh` pin 변경, provider 효과 활성화

## 9. owner가 결정해야 하는 것

1. 호스트 옵션과 **비용 상한**(월/회당)
2. 격리 등급(Lima / UTM / 전용 하드웨어)
3. teardown 책임자와 주기
4. C1c 이후 C2 착수 시점
5. 인증 계열(#81–#88)을 v0.11.0으로 분리할지 — C1 일정과 별개 결정

## 9a. C1a 후보 상태 (2026-11-21 읽기 전용 점검)

게이트를 실행하지 않고 호스트 준비 상태만 조회했다.

| 항목 | 결과 |
|---|---|
| Lima VM `mocktimer-release` | Running, `Linux aarch64`, uid 501(비root) |
| 도구 | git 2.43.0 · jq · python3 3.12.3 · sha256sum 확인 / **go·gofmt·rg 없음** |
| Mac 측 | `limactl`, `UTM.app`, `qemu-system-aarch64` 존재 |

판정: 이 VM은 native ARM64 비root 조건은 만족하지만 `g4-free-release-gate.sh`가 요구하는
`go`, `gofmt`, `rg`가 없어 **그대로는 C1b를 실행할 수 없다**. 또한 이름·용도가 다른 프로젝트
(`mocktimer`) 기준으로 실행 중이므로, C1b는 **별도 승인 + 전용 또는 정리된 VM**에서 수행해야
한다. 도구 설치나 VM 재사용 여부는 owner 결정 사항이며 이 문서는 그 승인이 아니다.

## 10. NOT VERIFIED

- 승인된 builder 없음 → C1a/C1b/C1c 미실행
- native ARM64/AMD64 릴리스 smoke 없음
- VM lifecycle·teardown 영수증 없음
- 이 문서의 어떤 문장도 릴리스 승인·운영 권한·pin 변경을 의미하지 않는다
