# 공통 정책 명시적 채택 — 2026-10-26

범위: 독립 `keycli` 저장소의 L1 문서·정책 lock 변경. 기존 JSON 구조와 `local_binding`을 보존했다. 변경 전 깨끗한 `main`의 HEAD는 `0a346da592d0c73d43fb72f30e37ccc95c757d22`.

## 기준과 직접 근거

- `source_commit`: `ae5e8378a652cd6d55e28d2bcdf7d113a106b251` → `652dec3cde2b93aa42432d57bb4ae7e6b804ed38` (로컬 workspace-infra commit).
- 로컬 바인딩 `../../../workspace-infra`는 이 저장소에서 `/Users/k/Developer/workspace-infra`로 해석됨을 확인했다. 누락 시 다른 checkout으로 fallback하지 않는다.
- 기존 lock의 두 hash는 옛 commit의 `git show` bytes와 일치했다. 새 lock의 두 hash도 새 commit의 committed bytes와 독립 SHA-256 비교했고 같은 정책의 현재 작업 트리 bytes도 일치했다.
- `policies/development-policy.md`: `750ae6f912f1c0fb46799773c95c79a4a46b27298d054797b19d6df6868f4eb5`.
- `policies/documentation-structure.md`: `d11b1eee289362f1ad703a5b76f9078afcb83dbd0136bb80a7413316e7a3a34d`.

## 검토한 의미 변화

- 사용자가 유일한 사람 개발자·결정권자다. 임시 executor/coordinator 배정, 실제 writer/resource claim, 컴포넌트·소스·문서 정본 역할을 구별한다. coordinator acceptance는 확인된 통합 범위이며 사람 attestation·실운영 성공을 대체하지 않는다.
- 같은 stage·scope·target·effects·budget의 기존 승인을 재사용한다. 제품별 엄격한 per-item/plan, pause/resume, 보존, 보안·권한·예산 경계와 실시간 writer claim은 유지한다. blocker는 필요한 사용자 결정, 실제 writer 충돌, 누락된 도구/바인딩, 부족한 증거로 구체화한다.
- `owner` frontmatter와 기존 status/`NEEDS_OWNER_DECISION` 필드는 유지한다. 이 채택은 코드·DB·schema·API 필드 rename이나 자동 enforcement 구현이 아니다.
- 옛 pin 이후 이미 커밋된 변화도 함께 채택한다: `git stash` 대신 WIP commit, heavy 검증의 infra `docs/verification.md` 큐·슬롯·진행/출력/증거 규칙 연결, 작은 직접 unit 검사의 dev-feedback 한계. 교차 저장소 meta planning 정본은 infra planning/tracks/catalog이며 과거 Vault/workflow 문서는 해당 정보의 역사 근거다. 아카이브 정리는 별도 사용자 승인 범위다.
- Linux-first·progressive context와 L1/L2/L3 문서 검증 비용 정책은 유지되는 계약이며 이번 변경으로 새로 구현된 도구가 아니다. 로컬 product 계약·금지·실행 명령을 공통 정책 채택으로 확대하지 않는다.

## 검증과 한계

`bash verify.sh --suite docs`, `--suite policy` (비밀 패턴 hygiene만 검사; lock 검증은 아님).

위 범위 검사는 exit 0으로 통과했다. `git diff --check`, 새 리뷰/HANDOFF 링크 및 task-only diff도 확인했다. native verifier의 검사 범위와 별개로 위 committed-blob identity를 직접 확인했다. 앱·Go/Swift build/test·Mac UI·실기기·설치·서비스·배포·원격 게시/merge는 이 검증에 포함하지 않는다. 키체인·`~/.kc`·비밀 값·실 push/서비스에는 접근하지 않았다.

독립 parent 검토에서 네 파일의 실제 diff·이 기록을 확인했고 발견 사항 없이 로컬 task-only commit을 승인했다. 원격 게시·merge는 수행하지 않는다.

현재 제품·운영 상태와 다음 작업은 [HANDOFF](../../HANDOFF.md)가 유지한다. 이 기록은 정책 채택 근거이며 제품 인수를 승격하지 않는다.
