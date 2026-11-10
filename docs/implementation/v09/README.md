---
status: NOT VERIFIED
date: 2026-11-10
baseline_keycli_main: 4c972d9815cf0f89f52dbcd91a772376c8c297dd
scope: design-only
---

# KeyCLI v0.9 구현 패키지 — GitHub / Cloudflare / Oracle

이 폴더는 v0.9 **설계와 구현 인수 경계**이며, 문서 자체가 운영 인수·출시의 증거가 아니다. KeyCLI `main`의 릴리스는 v0.8.0이다. K09-01~03은 [구현 Draft PR #47](https://github.com/camem2442/keycli/pull/47)의 후보 기능이며, K09-04~06 및 cross-repo 운영 연결은 `PROPOSED`다.

**2026-11-10 최신 관측:** PR #47 HEAD `e61beed451a5807023975f8650a57aa525097070`, GitHub Actions [Linux Run #42755608356](https://github.com/camem2442/keycli/actions/runs/42755608356) 완료·SUCCESS (Go 1.22; 동일 HEAD). GitHub WIF/Secret의 **실계정 provider 성공은 NOT VERIFIED**, CI 통과가 본선 merge/배포를 승인하지 않는다. PR #47 실제 CLI는 `kc delivery plan`, `kc delivery status`, 오프라인 `kc auth probe`이고, WIF 교환·GitHub Secret REST는 **신뢰 호스트용 내부 Go API**다. `kc delivery stage` / `kc rotate`는 아직 구현되지 않았다.

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
| `kc push --dry-run` | 기존 push 경로는 먼저 값을 조회. PR #47의 별도 delivery plan은 메타데이터 전용 | 기존 push는 유지, 새 stage/reconcile 안전경로 추가 |
| GitHub | PR #47 K09-01~03 구현 후보·CI SUCCESS, 실제 인증/쓰기 NOT VERIFIED | 실제 계정·호스트 인수 + K09-04 연결 |
| OCI | standard-vm #7 Draft, 소스·합성 테스트 | Instance Principal + OpenAI WIF (별도 실제 인수) |
| Cloudflare Development | 별도 이전·Auth Cutover 진행 중 | 기존 이전 작업과 같은 Worker 동시 변경 금지 |

비목표: 중앙 Vault/백그라운드 무차별 동기화, 전체 `KC_KEY` 전송, 운영 자동 배포·PR 병합, 무승인 토큰 생성·회수, 비용이 발생하는 실험 자동 수행, WIF가 실제로 사용 가능하다고 가정한 하드코딩.

## 하나의 실제 운영 흐름

`plan (값·원격 쓰기 없음) → workspace-infra admission → stage (대상별 effect) → verify (실제 관측) → 별도 Delivery 승인 → promotion → 관측 후 이전 키 폐기`.

KeyCLI의 `STAGED` 영수증은 절대 검증 PASS나 운영 배포 허가가 아니다. 외부 실행은 권한 없는 모델 생성 문자열이나 로컬 JSON만으로 허용하지 않는다.

## 문서 변경·병합 규칙

문서만 추가하는 브랜치의 PASS와 실제 기능 구현 PASS는 구분한다. GitHub Release / 호스트 바이너리 / Cursor Cloud pin / 운영 Worker / OCI VM은 이번 범위 밖이다. `[skip ci]` 사용 시 미실행 검증은 `NOT VERIFIED`로 기록한다.
