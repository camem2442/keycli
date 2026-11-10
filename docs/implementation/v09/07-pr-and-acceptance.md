---
status: NOT VERIFIED
scope: proposed-implementation-sequence
---

# 구현 PR 순서 / 테스트 / Git-first 인수

## 현재 계층과 충돌 회피

- `keycli main` 기준 커밋: `4c972d9815cf0f89f52dbcd91a772376c8c297dd` (2026-11-10 읽기 기준).
- 독립 Draft: [#29 수신자 증명](https://github.com/camem2442/keycli/pull/29), [#42 정책 pin](https://github.com/camem2442/keycli/pull/42), [#45 CI guard](https://github.com/camem2442/keycli/pull/45); [#25](https://github.com/camem2442/keycli/pull/25)는 DO NOT MERGE 검증 후보. [#40](https://github.com/camem2442/keycli/pull/40)은 기존 보존 문서. 다른 브랜치 HEAD를 자동으로 병합하거나 재기준화하지 않는다.
- [workspace-infra #239](https://github.com/camem2442/workspace-infra/pull/239)는 read-only 중복 CI 분류, [#230](https://github.com/camem2442/workspace-infra/pull/230)은 독립 CF Auth Cutover. 중복 검증/배포 엔진 개발 금지.
- [standard-vm #7](https://github.com/camem2442/standard-vm/pull/7)은 Oracle adapter Draft/source-only. 새 KeyCLI 코드가 OCI 인스턴스를 기동할 수 있는 권한을 가진 것으로 간주하지 않는다.

## PR 분할 (단일 Writer, 하나씩 인수)

| PR | 저장소 | 소유 파일 후보 | 정확한 Gate |
|---|---|---|---|
| K09-01 | keycli | `internal/delivery/{plan,profiles}.go`, CLI plan, fake tests | Plan secret calls 0 / network 0 / explicit target |
| K09-02 | keycli | GitHub adapter / WIF metadata·claim probes | GitHub repo/org scope 분리 / OIDC claim mismatch 거부 |
| K09-03 | keycli | operation journal, reconcile, tests | fsync/lock/timeout/duplicate/STALE |
| K09-04 | keycli | Cloudflare version-only adapter / fake wrangler | active deploy 불변 / bulk stdin / deploy 0 |
| K09-05 | keycli | OCI WIF probe / fake IDCS signer | wrong audience/instance/tenancy deny |
| WI-KC-01 | workspace-infra | trusted KeyCLI receipt consumer | authority false 유지, exact HEAD/host/proof |
| SV-OCI-02 | standard-vm | #7 이후 WIF + Runner 한계 계약 | G1~G7, G4≠G6, live는 별도 허가 |
| K09-06 | keycli | credential rotation coordinator | 이전 키 마지막 폐기 / 중단 복구 |

K09-01~04를 v0.9.0 첫 릴리스 후보로 묶되, 릴리스 자체는 독립 승인. K09-05/WI/SV는 각 provider 운영 인수 전까지 기능 구현 PASS 이상을 주장하지 않는다. 기존 호스트에 설치된 v0.8.0, Cursor Cloud v0.7.2 pin은 해당 호스트 실제 인수 전까지 그대로 둔다.

## 공통 테스트 매트릭스

| 대상 | 합성 테스트 | 실제 호스트 검증 | 운영 권한 |
|---|---|---|---|
| Local Plan | 정책·중복 alias·무권한·비밀 조회 없음 | Mac `kc doctor` + CLI fake | 불필요 |
| GitHub WIF | issuer/audience/repository/ref/workflow_ref/env 실패 | pinned trusted Job 1턴 | WIF 설정 승인, 비용 gate |
| GitHub Secret | repo vs org target, stdin, no key in argv | synthetic test repository | 명시 Repo mutation |
| Cloudflare | wrong account, bulk single version, never deploy | disposable Worker, before/after active | 별도 staging effect 승인 |
| Oracle | Instance Principal/JWT claim fake | 실제 OCI VM 확보 후 | Free envelope + VM/Job 승인 |
| Reconcile | unknown timeout, retry, concurrent lock | bounded single target | 새로운 효과 승인 불필요: read-only만 |
| Rotation | candidate valid, old key still active | 단일 비운영 key + 두 consumer | revoke 별도 승인 |

## 실행 순서(구현 에이전트)

1. 최신 remote main, 활성 작업 트리, 진행 PR과 `AGENTS.md` 정책 pin 검사. 다른 WIP 변경 감지 시 해당 파일 변경하지 않음.
2. 해당 PR의 source-only 범위 및 `verify.sh --suite` 매핑. 실행 전 자동 Actions trigger / Linux 호스트 비용 확인. 수동 Dispatch·Oracle VM·Cloudflare 호출 금지.
3. 임시 HOME, file store, fake keychain, fake gh/wrangler/OCI로 재현 → 부정 테스트부터 구현. `go test` / `verify.sh` 정확한 suite; 출력·argv·파일 Secret leak 검사.
4. 독립 branch/PR에 소스·테스트·계약 수정. 정확한 HEAD 기반 검증 receipt. docs-only `[skip ci]` 사용 시 Linux CI NOT VERIFIED(=PASS 아님).
5. Solo Maintainer가 실제 인수/병합 결정을 내림. 타인의 APPROVED는 개인 저장소의 필수 게이트로 추가하지 않고 기술 증거·권한을 따로 기록.
6. 한 Provider에 대해 throwaway 인증/리소스 테스트를 *별도로* 승인 받은 뒤 실 연동. 서비스 계정·지불계정·무료 한도·비용 모두 확인.

## 완료 정의

- `COMPLETE`: 같은 HEAD의 지정한 테스트와 실호스트 검증이 해당 범위에서 완료됨. Source COMPLETE는 운영 COMPLETE 아님.
- `PARTIAL`: 제한된 범위 통과, 전체 acceptance 미충족.
- `BLOCKED`: 명확한 필수 권한/자료/호스트/비용 조건 부재.
- `DEFERRED`: 이번 릴리스 의도적 제외.
- `NOT VERIFIED`: 테스트 미실행 또는 신뢰된 증거 없음.

보고에는 ref/HEAD/base, 파일별 변경, test command/counters, 실행한 외부 effect, denied effect, 호스트/배포·키 불변 증거, blocker 및 후속 요청을 포함한다. 시크릿 ID/토큰 원문을 포함하지 않는다.
