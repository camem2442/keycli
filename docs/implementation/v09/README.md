---
status: NOT VERIFIED
date: 2026-11-10
baseline_keycli_main: 4c972d9815cf0f89f52dbcd91a772376c8c297dd
scope: design-only
---

# KeyCLI v0.9 구현 패키지 — GitHub / Cloudflare / Oracle

이 폴더는 **향후 구현 계약과 작업 지시**이며, v0.9 기능이 출시되거나 운영 환경에서 인수되었다는 증거가 아니다. 개별 문서의 명령 및 Go 패키지는 `PROPOSED`이고, 실제 `kc` v0.8.0의 명령과 혼동하지 않는다.

## 읽기 순서

1. [01-ownership.md](01-ownership.md): KeyCLI / workspace-infra / standard-vm 권한·신뢰 경계
2. [02-cli-and-go-contracts.md](02-cli-and-go-contracts.md): 명령, 설정·검증, Go 인터페이스
3. [03-github-wif.md](03-github-wif.md): GitHub OIDC / Actions / 저장소별 Secret 처리
4. [04-cloudflare-inactive-versions.md](04-cloudflare-inactive-versions.md): Worker 비활성 버전과 별도 승격
5. [05-oracle-wif-runner.md](05-oracle-wif-runner.md): OCI Instance Principal / OpenAI WIF / 임시 러너
6. [06-rotation-and-recovery.md](06-rotation-and-recovery.md): 회전·상태 머신·재조정·복구
7. [07-pr-and-acceptance.md](07-pr-and-acceptance.md): 최소 PR 시퀀스, 테스트·비용 게이트
8. [08-mac-agent-prompt.md](08-mac-agent-prompt.md): Mac 로컬 코딩 에이전트 독립 실행 프롬프트
9. [SOURCES.md](SOURCES.md): 기존 정본·상태 증거·공식 API 출처

## 정본 우선순위

- `AGENTS.md`, `dependencies/workspace-policy-lock.json`, `docs/contracts/cli.md`, `docs/contracts/security-model.md`, `docs/contracts/transfer.md`가 **현재 구현/정책**의 정본이다. 이 폴더는 그 문서를 소급해 변경하지 않는다.
- `docs/planning/v08-multi-agent-auth-infrastructure.md`와 `docs/planning/v080-release-evidence.md`는 **v0.8 이력과 호스트별 인수 범위**의 정본이다.
- v0.9 계약이 실제 코드로 구현되고 검증된 후에만 `contracts/` 및 `guides/` 정본으로 승격한다. 본 폴더는 미래 설계에 대한 상위 권한이 아니다.
- 타 저장소의 실행권한은 각 저장소 정본을 따른다: [workspace-infra Goal Verification](https://github.com/camem2442/workspace-infra/blob/main/docs/contracts/canonical-goal-pr-verification-v1.md), [standard-vm Oracle adapter (Draft #7)](https://github.com/camem2442/standard-vm/pull/7).

## 기준 상태와 비목표

| 영역 | 2026-11-10 확인 | v0.9 목표 |
|---|---|---|
| KeyCLI | v0.8.0 released, main `4c972d9` | 안전한 전달 Plan/Stage/Verify |
| KeyCLI `kc push cloudflare` | 내부 `wrangler secret put` → 즉시 배포 | 비활성 버전 Stage 기본 |
| `kc push --dry-run` | 현재 resolver가 먼저 비밀값 조회 | Plan 단계 값 조회 0회 |
| GitHub | 제한된 `gh secret set` 및 `kc ci-guard` Draft #45 | 정확한 repo/env 선택 + OpenAI WIF |
| OCI | standard-vm #7 Draft, 소스·합성 테스트 | Instance Principal + OpenAI WIF (별도 실제 인수) |
| Cloudflare Development | 별도 이전·Auth Cutover 진행 중 | 기존 이전 작업과 같은 Worker 동시 변경 금지 |

비목표: 중앙 Vault/백그라운드 무차별 동기화, 전체 `KC_KEY` 전송, 운영 자동 배포·PR 병합, 무승인 토큰 생성·회수, 비용이 발생하는 실험 자동 수행, WIF가 실제로 사용 가능하다고 가정한 하드코딩.

## 하나의 실제 운영 흐름

`plan (값·원격 쓰기 없음) → workspace-infra admission → stage (대상별 effect) → verify (실제 관측) → 별도 Delivery 승인 → promotion → 관측 후 이전 키 폐기`.

KeyCLI의 `STAGED` 영수증은 절대 검증 PASS나 운영 배포 허가가 아니다. 외부 실행은 권한 없는 모델 생성 문자열이나 로컬 JSON만으로 허용하지 않는다.

## 문서 변경·병합 규칙

문서만 추가하는 브랜치의 PASS와 실제 기능 구현 PASS는 구분한다. GitHub Release / 호스트 바이너리 / Cursor Cloud pin / 운영 Worker / OCI VM은 이번 범위 밖이다. `[skip ci]` 사용 시 미실행 검증은 `NOT VERIFIED`로 기록한다.
