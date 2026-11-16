---
status: PARTIAL
owner: keycli
date: 2026-11-16
keycli_main: 2897465f7abd9ca205d774c42c26cd95d5b9899a
infra_main: 4e9e103b6c53b653a830ee5f7c17c3a79b7ee4d8
scope: source and PR reconciliation; no operating grant
---

# KeyCLI 현재 정본 — v0.10 소스·운영 인수 로드맵

**2026-11-16 GitHub 조회 기준.** [기존 로드맵](roadmap.md)의 P0~P2와 M1~M6는 역사적 구현 기록이다. PR HEAD와 main이 변하면 모든 검증·통합 판정을 새로 확인한다. 상태는 COMPLETE / PARTIAL / BLOCKED / DEFERRED / NOT VERIFIED로만 표기한다.

## 제품 목표와 현재 버전

사람·코딩 에이전트의 자격증명을 이름과 대상 범위로 해석·주입하고, 원격 공급자 변경은 독립 호스트의 Goal/Grant/Writer Claim을 확인한 뒤 한 효과만 실행한다. KeyCLI = Secret resolver, injection, scoped transport, effect gateway, journal. workspace-infra = Goal·Claim·Grant·PR/검증 권한·호스트 입장·공유 CI 큐. standard-vm = VM·runner host lifecycle. **KeyCLI 안에 2번째 Vault/Authority/Scheduler를 만들지 않는다.**

| 층위 | 근거 | 실제 판정 |
|---|---|---|
| 정식 릴리스/설치 | [v0.8.0](https://github.com/camem2442/keycli/releases/tag/v0.8.0) (2026-11-08) | COMPLETE, Mac 로컬 pin v0.8.0 / Cursor Cloud 기본 pin v0.7.2 |
| KeyCLI 개발 소스 | [main 2897465f](https://github.com/camem2442/keycli/commit/2897465f7abd9ca205d774c42c26cd95d5b9899a), [통합 #69](https://github.com/camem2442/keycli/pull/69) | v0.9/0.10 source merge COMPLETE, v0.10 tag/release/운영 NOT VERIFIED |
| Linux post-merge | [Actions #43695782808](https://github.com/camem2442/keycli/actions/runs/43695782808) | linux · linux-arm64-release 두 Job 성공; Mac Keychain/실 공급자 아님 |
| Infra 메인 | [4e9e103b](https://github.com/camem2442/workspace-infra/commit/4e9e103b6c53b653a830ee5f7c17c3a79b7ee4d8) | Gateway Authority 소스·합성 경합, 실 운용 권한 별도 |
| Infra M4.6 | [#466](https://github.com/camem2442/workspace-infra/pull/466) 병합 | 비실행 disposable byte lifecycle source COMPLETE; 실제 운영 설치 NOT VERIFIED |
| Linux admission | [Infra #487](https://github.com/camem2442/workspace-infra/pull/487) 병합 | bounded Linux R1 source/one-shot proof; 지속적 trusted profile enrolment NOT VERIFIED |

### 계층별 구현

1. **A — 로컬 인증 M1~M6:** env/file/Keychain, age, clipboard, Git helper, MCP metadata-only, Policy/Audit. v0.8 배포 COMPLETE; 소비자별 E2E PARTIAL.
2. **B — K09 Delivery:** `internal/delivery`, `internal/identity/githubwif`, `internal/identity/ociwif`, `kc delivery plan/status`, `kc auth probe`. Source COMPLETE, WIF/provider live NOT VERIFIED.
3. **C — K09 Cloudflare:** `internal/delivery/cloudflaredelivery`, inactive-version stage, Free-only preflight. Source COMPLETE, live Worker/env·budget·traffic binding NOT VERIFIED.
4. **D — K10 Rotation:** `internal/rotation`, `kc rotation plan/status/submit/reconcile`, issuer/revoker/provider identity interfaces. Source COMPLETE, real key retire NOT VERIFIED.
5. **E — HostBridge/Gateway:** `internal/hostbridge`, `internal/effectgateway`, `cmd/kc-gateway`, `cmd/kc-gateway-r0`. Signed intent·Writer generation·journal·lost reply·synthetic cross-host tests COMPLETE, operating admission PARTIAL.
6. **F — Dev/Operating:** `.github/workflows/release.yml`, `scripts/verify-dev-release.sh` and M4.6. Dev source/isolated artifact PARTIAL; real release/install/operation BLOCKED.

**교정:** main에는 `internal/effectgateway/providers`의 Cloudflare/GitHub/Oracle 공급자 어댑터 코드가 이미 있다. `-provider live`는 operational 빌드 채널과 `KC_PROVIDER_EFFECTS=enabled`, 보호된 Authority/Product/Read 설정이 모두 필요하다. 소스 존재나 Linux CI 성공은 실제 토큰·원본 공급자 효과·same-UID 격리의 증명이 아니다.

## Q1 — 열린 PR 13건 vs main Git blob 비교

각 PR `pulls/N/files` 경로의 Blob SHA를 main Git tree의 **531개 경로**와 비교했다. 숫자는 **동일 / 상이 / main에 경로 없음** 순서. 상이는 누락된 기능의 증거도, 이전 브랜치가 더 안전하다는 뜻도 아니다. 과거 스택 전체 동등성은 여기서 주장하지 않는다.

| PR | Blob 분류 | 통합 판단 |
|---|---|---|
| [#52](https://github.com/camem2442/keycli/pull/52) K10-03 | 2/0/0 | 2파일 exact duplicate → HEAD 재조회 후 중복 종료 |
| [#59](https://github.com/camem2442/keycli/pull/59) CF Free | 0/3/0 | #64→#69 흡수 계열이지만 main bytes 변경; 과거 스택 직접 병합 HOLD |
| [#61](https://github.com/camem2442/keycli/pull/61) Integration | 3/12/0 | DO NOT MERGE 검증 전용, 현재 main 검증으로 자동 승격 금지 |
| [#62](https://github.com/camem2442/keycli/pull/62) Journal | 0/1/0 | 통합 계열, 최신 main 테스트가 상이; 별도 merge HOLD |
| [#63](https://github.com/camem2442/keycli/pull/63) CF expiry | 1/2/0 | 통합 계열, 최신 main provider 내용 상이; 의미 비교 전 HOLD |
| [#70](https://github.com/camem2442/keycli/pull/70) Flip3 CI | 0/2/2 | 미등록 전용 runner, 최신 main 충돌; 그대로 merge 금지 |
| [#71](https://github.com/camem2442/keycli/pull/71) Draft no-queue | 0/1/0 | skip/coalescing 유용, hosted ubuntu-latest 경로 남음; 통합 후보 HOLD |
| [#46](https://github.com/camem2442/keycli/pull/46) v0.9 문서 | 0/1/16 | 초기 계획, 현행 정본과 중복; 역사 기록으로 선택적 이전 |
| [#45](https://github.com/camem2442/keycli/pull/45) CI guard | 0/1/4 | infra 공용 classifier와 중복 소유 여부 확인; DEFERRED |
| [#42](https://github.com/camem2442/keycli/pull/42) policy lock | 0/2/0 | 원래 infra policy pin/digest 재자격화 후 판단; HOLD |
| [#40](https://github.com/camem2442/keycli/pull/40) closeout docs | 0/1/1 | 오래된 HANDOFF와 역사 문서, Ready 유지보다 Draft HOLD |
| [#29](https://github.com/camem2442/keycli/pull/29) attestation | 0/5/8 | 미통합 보안 경로, trust issuer를 별도 Goal로 검증할 때 채택 |
| [#25](https://github.com/camem2442/keycli/pull/25) Integration | 8/4/0 | DO NOT MERGE 검증 전용; 현재 verify 계약과 차이 보존 |

#64/#65/#66/#68 원본 PR은 closed/unmerged이지만 그 변경 계열이 #69 통합을 거쳐 main에 들어왔다. 이는 개별 PR이 병합됐다는 뜻이 아니다. 정확 중복만 HEAD 재조회·근거 댓글 후 close; 그 밖에 코드 차이가 있는 오래된 PR은 새 main에서 독립 재구성·검증하기 전까지 Draft 또는 HOLD.

### 같은 날 변경된 PR 상태 (감사 이후 전환)

- [#52](https://github.com/camem2442/keycli/pull/52)는 소유한 2개 파일의 main blob 동일성을 원본으로 재확인한 뒤 설명 댓글을 남기고 **CLOSED / NOT MERGED**. 기존 branch와 역사 기록 유지.
- [#40](https://github.com/camem2442/keycli/pull/40)은 오래된 HANDOFF diff 및 최신 HEAD 적격 검증 부재를 기록한 뒤 **DRAFT / HOLD**로 전환. 과거 Linux Job 성공은 현재 main에 대한 완료 근거가 아니다.
- [#72](https://github.com/camem2442/keycli/pull/72)는 이 로드맵의 문서 전용 신규 **DRAFT** PR이다. 위 13행은 #72 생성 **이전**의 감사 대상이며, #52 이력 보존을 위해 표에서 삭제하지 않는다.
- 상태 전환 후 GitHub 재조회: 열린 PR **13건** (Draft **13건**, 일반 **0건**). 이후 변동은 이 기록을 자동으로 갱신하지 않으며 새 감사가 필요하다.

## Q2 — KeyCLI vs workspace-infra 구현 차이와 미완료 게이트

| 경계 | KeyCLI 현행 기능 | Infra 소유 | 미검증 조건 |
|---|---|---|---|
| Plan/Policy | profile, alias/Policy, canonical target digest | original Goal Registry/Grant/known effects | 원본 Grant/Plan/HEAD/리소스 identity를 trusted host 독립 검증; allowlist 자체는 작업 승인 아님 |
| HostBridge | operation whitelist, GatewayBinding, HostAuthorize, host-owned identity | `tooling/keycli_host_admission.py`, `tooling/keycli_gateway_authority.py` | 실제 peer/host Grant/Writer snapshot, 임의 callback 거부, 효과 직전 authority 확인 |
| Effect Gateway | HMAC session, durable journal, stale Writer, unknown latch, reconcile | `tooling/keycli_guarded_execution.py`, SQLite claim/reservation, settlement | Unix peer·borrowed lock FD·journal/claim race·lost reply에 대한 실제 호스트 영수증 |
| Provider | `internal/effectgateway/providers` 실 호출 코드 + build/runtime lock | Goal/Grant/권한·원본 host 보관 | 분리된 credential 권한, disposable exact target, real GET·issuer/revoker·unknown reconcile |
| Isolation | Mac·Flip3↔Lima synthetic fencing | 별도 host isolation/OS identity | Lima guest UID 격리 PASS이나 Mac same-UID의 sudo/VM disk 우회 가능. PRODUCT_CREDENTIAL_ISOLATION NOT VERIFIED |
| Profiles | 제품 원본 Go test, receipt | Linux [#487](https://github.com/camem2442/workspace-infra/pull/487), D1 [#508](https://github.com/camem2442/workspace-infra/pull/508), D2 [#519](https://github.com/camem2442/workspace-infra/pull/519), Mac [#518](https://github.com/camem2442/workspace-infra/pull/518) | 독립 trusted revision custody D2~D5, native Seatbelt/libproc M0~M3, 최신 namespace 소스 재검증 |
| Install | v0.8 release/pin, v0.10 dev candidates | Infra M4.6 isolated artifact CAS + rollback | signed/pinned original bytes, 실제 설치 서비스 native owner, operating eligibility 미검증 |
| Consumers | `kc exec --require --only`, MCP policy/audit, Git helper | Harness Task/PR/merge grant | Cursor/Claude/Codex 각 1-turn allowed/denied/missing/audit 원본, token lookup ≠ PR merge authority |

소스: [Effect Gateway](../contracts/effect-gateway-v1.md) · [HostBridge](../contracts/hostbridge-v1.md) · [Infra Authority](https://github.com/camem2442/workspace-infra/blob/main/docs/contracts/keycli-gateway-authority-v1.md) · [Infra Host Admission](https://github.com/camem2442/workspace-infra/blob/main/docs/contracts/keycli-host-admission-v1.md) · [S4 isolation](../reviews/2026-11-11-s4-gateway-host.md).

## Q3 — G1~G6 병렬 트랙과 병합 의존성

```text
G1  KEYCLI-ROADMAP-MAIN-RECONCILE-R1 (현재 문서/PR 감사)
├─ G2 KEYCLI-CI-NOQUEUE-R1            최신 main CI / no paid fallback
├─ G3 KEYCLI-TRUSTED-ADMISSION-R2
│  ├─ G3-L Linux profile revision custody D0-D5
│  ├─ G3-M Mac Seatbelt/libproc native M0-M3
│  └─ G3-X original Grant/Claim/peer/journal E2E (L + M 이후)
├─ G4 KEYCLI-V010-DEV-RELEASE-R1      locked, disposable binary rollback
├─ G6-S KEYCLI-AGENT-ADOPTION-R1      synthetic-only agent acceptance
└─ G5 KEYCLI-PROVIDER-PILOT-R1        G3-X + G4 + 별도 owner grant 이후
    └─ G6-L actual consumer one-turn + operating release decision
```

**독립 소스 병렬:** G2, G3-L, G3-M, G4, G6-S. **순차:** G3-X after L/M, G5 after G3-X/G4, real G6 after G5. 서로 다른 worktree는 허용하되 **Flip3 물리 단일 슬롯**, 원본 Claim/Journal, 런타임 profile, Mac 설치 pin 변경은 기존 Infra 자원 소유권으로 직렬화한다. Flip3=bounded portable/source; Linux VM=필수 격리·Go 회귀; Mac=native 최종 인수. 유료 GitHub-hosted 자동 fallback 금지.

| Goal | 최소 PASS 조건 / HOLD 사유 |
|---|---|
| G1 | 13 PR 원본/blob audit, docs links·현행 snapshot review; 문서 편집만으로 operating authority 불가 |
| G2 | current main 기반 single owning verifier, Draft no-job, 최신 Ready HEAD, 실제 등록 러너에서 수행된 Go 원본. Skip/queued/cancelled ≠ PASS |
| G3 | 독립 신뢰 profile pin custody, original Grant/Claim/Writer, Mac native process peer/Seatbelt·경합 부정 테스트. Linux R1 재사용으로 Mac PASS 선언 금지 |
| G4 | exact source/tag, hash, dev provider lock, disposable prepare/verify/select/status/rollback, v0.8 설치 불변. Fixture ≠ 운영 활성화 |
| G5 | 정확한 disposable provider 하나, bounded 권한/예산, before/after GET, UNKNOWN_EFFECT read-only reconcile, 별도 issuer/revoker grant |
| G6 | 소비자별 pinned binary, allow/deny/missing, no child on reject, audit no-values, 실제 단일 turn과 host receipts |

### 다음 인수 금지 규칙

1. 이 문서/PR은 제품 코드·권한·CI/런타임 설정을 바꾸지 않는다. docs 정적 검사만으로 required owning CI/review를 무효화하지 않는다.
2. #70의 전용 미등록 runner를 다른 저장소의 `flip3-a14-wi` 라벨로 억지 매핑하지 않는다. #71의 draft skip만으로 유료 hosted minutes가 없어졌다고 주장하지 않는다.
3. KeyCLI는 infra의 Goal/Claim/Authority를 재사용하고 자체 중앙 승인/분산 Queue를 만들지 않는다.
4. G4 dev binary에 실 공급자 잠금을 풀거나 Mac v0.8.0/Cloud v0.7.2 기존 pin을 교체하지 않는다.
5. 기존 `kc push cloudflare`는 즉시 배포이고 새로운 inactive-version stage는 다른 계약이다. 실제 운영 Worker에 시험하지 않는다.
6. Secret 조회 권한은 push/deploy/revoke/PR approve/merge 권한을 의미하지 않는다. 자동 vault sync·일괄 폐기·숨은 provider retry 금지.

이 문서의 완료는 **소스·계획 정합**에 한정된다. 실제 Keychain/provider effect, 원격 CI dispatch, tag/release/설치·운영 권한은 미실행이다.
