---
status: PARTIAL
owner: keycli
date: 2026-11-19
keycli_main: 352e12d72a3fc7189e7a4049c5643ce1d593d41e
supersedes_status_of: keycli-current-roadmap-2026-11-16.md
scope: roadmap status reconciliation; no operating grant, no release authorization
---

# KeyCLI 현재 정본 — 2026-11-19

[2026-11-16 정본](keycli-current-roadmap-2026-11-16.md)의 **상태표만** 이 문서가 대체한다.
11-16 문서의 Q1(PR blob 감사)·Q2(경계)·Q3(G1~G6 구조)와 G/E 게이트 정의는 계속 유효하다.
이 문서는 제품 코드·권한·CI/런타임 설정을 바꾸지 않는다.

## 현재 위치

| 층위 | 근거 | 판정 |
|---|---|---|
| 공식 릴리스 | [v0.8.0](https://github.com/camem2442/keycli/releases/tag/v0.8.0) `ad30607` (2026-11-08) | COMPLETE — Latest |
| 공개 prerelease | `v0.10.0-rc.1000`·`rc.1001` `80d1408` (2026-11-18, Linux ARM64 전용) | COMPLETE(게시) / Mac 자산 없음 |
| v0.10.0 정식 | 태그·Release 없음 | NOT VERIFIED |
| 개발 소스 | `origin/main` `352e12d` (#76) | COMPLETE(소스) |
| 릴리스 자동화 | `.github/workflows/release.yml:50` classify `if: false` | BLOCKED — 의도적 차단, 해제 PR 없음 |
| Mac 운영 경로 | `~/.local/bin/kc` = `v0.10.0 (352e12d)`, `036e0840…7372` | COMPLETE — **일회성**, 공식 pin 아님 |
| Mac Keychain 경로 | `kc doctor --json` → `keychain: unavailable`; `backends = link/env/store` | NOT VERIFIED — keychain 백엔드가 config에 없다. Aqua 세션·로그인 키체인 자체는 정상 |

**공식 pin과 실기기 상태가 다르다.** 문서·스크립트의 기본 pin은 v0.8.0이고, 이 Mac에는
v0.10.0 dev 후보가 일회성 승인으로 올라가 있다. 설치 기록은 [HANDOFF](../../HANDOFF.md)의
2026-11-19 항목이 소유한다.

## 2026-11-16 이후 변동

- **#73 병합** — `test.yml`이 self-hosted `Linux/ARM64/keycli-ephemeral-vm` + reviewed PR/HEAD/base/admission 입력으로 전환. 실제 독립 ephemeral VM admission 영수증은 이 문서 범위 밖이다.
- **#75 병합** — release.yml의 classify Job `if: false`. 태그 push·수동 dispatch 모두 릴리스 Job을 배정하지 않는다.
- **#76 병합** — `scripts/dev_cas_pilot.py`, `scripts/dev_cas_native_e2e.py`, `scripts/dev_release_strict.py`와 owner acceptance 가이드. 소스/격리 파일럿이며 운영 인수가 아니다.
- **`v0.10.0-rc.1000`/`rc.1001` 게시** — 둘 다 `80d1408`, Linux ARM64 자산만 있다.
- **열린 PR 12건, 전부 Draft** — #71 #70 #63 #62 #61 #59 #46 #45 #42 #40 #29 #25. 11-16 표의 13건과 다르다(#52 종료, #72 병합). 이 수치는 자동 갱신되지 않는다.
- **Mac 1회 설치** — 아래.

## Mac 1회 설치 (2026-11-19)

증거: `/Users/k/Developer/_management/evidence/keycli-mac-install-v010-20261119.OxeNKC/`
(`install-authorization.json`, `preflight.json`, `apply-receipt.json`, `readback.json`,
`kc-v0.8.0.rollback`).

| 항목 | 사실 |
|---|---|
| 후보 | 비공개 artifact `14489505421`, run `44433064454`, source `352e12d`, `036e0840…` |
| 이전 | `v0.8.0 (ad30607)`, `fd84b597…` — 백업으로 보관 |
| 적용 | `apply-receipt.json` `result=COMPLETE`, 진입점 `/Users/k/bin/kc` → `/Users/k/.local/bin/kc` |
| 상태 | store 메타데이터 33개 동일, `doctor`·`ls --json` 정상, 보호 파일 불변 |
| Keychain | 교체 전후 모두 `unavailable` — 회귀는 아니지만 **실경로 NOT VERIFIED** |

이 설치가 증명하지 **않는** 것:

1. **공식 pin 갱신(R6)이 아니다.** `scripts/install.sh` 기본 pin은 v0.8.0 그대로다.
2. **R4 자동 적용 연결이 아니다.** 임시 스크립트(`mac-install.py`)로 수행된 일회성 작업이다.
3. **1회성 강제는 이 실행에서 증명되지 않았다.** 재실행 거부는 `safe_target()`의 대상 해시 드리프트 가드가 발동한 것이고, 승인 소비 기록은 쓰기만 하고 읽는 검사가 없었다.
4. **복원 바이트 해시가 영수증에 없다.** `preflight.json`에 스크래치 복원본의 `restored_sha256` 필드가 없어, 복원 주장은 stdout 서술과 `duplicate_restore_refused` 플래그에 의존한다.

## 남은 작업과 순서

의존성 기준. 완료 판정은 "실행한 검사"만 인정하며, skip·queued·cancel은 PASS가 아니다.

### A. 정합성 (선행 없음)

| ID | 작업 | 상태 |
|---|---|---|
| A1 | `origin/main` 기준 작업 트리·`dist/kc` 재빌드 | 이 문서의 브랜치 |
| A2 | HANDOFF·roadmap에 Mac 1회 설치 기록, R6 pin과 분리 | COMPLETE |
| A3 | 11-16 감사 갱신(PR 12건, #73/#75/#76, rc) | COMPLETE — 이 문서 |
| A4 | preflight `restored_sha256` 필드 | 기존 영수증 소급 불가 — 2026-11-19 설치 영수증에는 없다. 신규 경로는 B2 도구가 기록한다 |

### B. 설치 경로 제품화 — R4

| ID | 작업 | 완료 조건 |
|---|---|---|
| B1 | 1회성 소비를 대상 변조 **전에** 명시 검사 | COMPLETE — `scripts/install_candidate.py`의 `_consume()`. 대상 해시를 `OLD_SHA`로 되돌린 재실행이 "already consumed"로 거부되는 회귀 테스트 포함 |
| B2 | 임시 `mac-install.py` → 저장소 소유 도구 | COMPLETE(도구) — 후보·호스트·이전 해시 바인딩, 준비/적용/롤백 영수증, `restored_sha256`·바이트 복원 증명, 실패 시 자동 복원. 실제 운영 경로 사용은 별도 승인 필요 |
| B3 | R4 자동 적용 연결 | COMPLETE(도구) — `prepare(artifact_dir=...)`가 `SHA256SUMS` + `ssh-keygen -Y verify`로 producer receipt를 검증하고, 허용 키 다이제스트·artifact/run/source SHA를 승인서와 대조한 뒤에만 preflight를 만든다. 실제 운영 실행은 별도 승인 필요 |
| B4 | 이 Mac Keychain 실경로 | BLOCKED — 진단 완료: 세션은 Aqua이고 로그인 키체인은 존재하지만 `~/.config/kc/config.toml`에 `[backend.keychain]`이 없어 백엔드가 로드되지 않는다. config 변경과 일회용 `kc.test.*` 쓰기는 승인 필요 |

### C. 릴리스 자동화 — G4

| ID | 작업 | 현재 |
|---|---|---|
| C1 | G4-A 신뢰 ephemeral 빌더 선정·등록·teardown·비용 | 미완 — 선행 조건 |
| C2 | G4-B classify `if: false` 해제(별도 PR) | 미완 — 수동+reviewed SHA+immutable tag 계약 필요 |
| C3 | G4-C rc 산출물 정리 | rc.1000/1001 게시됨, manifest·receipt·Darwin native smoke 미정리 |
| C4 | G4-D stable 자동 발행 | 미완 — signed bounded approval intent, PR-14 인수 |
| C5 | E0~E6 원본 증거 실행 | 문서상 미실행 |

### D. 신뢰·격리 — G3

| ID | 작업 | 현재 |
|---|---|---|
| D1 | G3-L Linux profile custody D0–D5 | Linux R1 proof만 |
| D2 | G3-M Mac Seatbelt/libproc native M0–M3 | NOT VERIFIED — Mac 최종 인수 지점 |
| D3 | G3-X original Grant/Claim/Writer/peer/journal E2E | D1·D2 이후 |

### E·F. 공급자 효과 → 소비자

| ID | 작업 | 선행 조건 |
|---|---|---|
| E1 | G5 disposable provider 1개, 예산 한도, before/after GET, `UNKNOWN_EFFECT` reconcile, issuer/revoker 분리 | D3 + C4 |
| F1 | G6-S synthetic 소비자 1턴 (allow/deny/missing/audit) | 없음 — 지금 가능 |
| F2 | G6-L 실제 소비자 1턴 | E1 |

### G. 제품 기능 격차

| ID | 격차 | 상태 |
|---|---|---|
| G1 | **K10 rotation CLI 배선** — `internal/rotation`은 라이브러리만, CLI 미연결 | v0.10의 실사용 표면 미완 |
| G2 | K09 live (WIF provider, Cloudflare inactive stage) | Source COMPLETE / live NOT VERIFIED |
| G3 | M4.5 잔여 (gh `--user`·`--app`, Cursor 팀 엔드포인트, alias 실 PUT) | NOT VERIFIED |
| G4 | `export --format dotenv` clip 실측, 서드파티 플러그인 | integration/데모 수준 |

### H. PR·정책 정리

- #42 policy pin — infra pin/digest 재자격화 후 판단.
- #59 #62 #63 #71 #70 — 최신 main에서 독립 재구성 여부 결정. 현행 HOLD 유지.
- #40 #46 #45 #61 #25 — 종료·역사 보존 판단.

## 다음 인수 금지 규칙 (11-16 문서 유지 + 추가)

1. 이 문서만으로 operating authority·릴리스 승인·pin 변경을 주장하지 않는다.
2. C1 없이 `release.yml`의 차단을 해제하지 않는다. 태그 push나 소스 CI 성공은 릴리스 근거가 아니다.
3. B2 도구를 실제 운영 경로에 쓰려면 별도 owner 승인과 그 승인에 묶인 1회 실행이 필요하다.
4. Mac 일회 설치를 R6 pin 갱신·R4 자동화 완료로 확대 해석하지 않는다.
5. HOLD PR(#59/#62/#63/#70/#71/#42)을 검증 없이 병합하지 않는다.
6. 실 운영 Worker·시크릿·공개 태그/Release를 시험 대상으로 쓰지 않는다.
