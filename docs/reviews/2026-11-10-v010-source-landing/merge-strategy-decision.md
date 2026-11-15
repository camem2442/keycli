---
status: historical
owner: keycli-delivery
last_verified_commit: 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace
---

# 병합 전략 결정

## 선택지

### Option A — 소유 PR 순차 병합

`#47 → #48 → #50 → #55 → #59 → #49 → #51 → #52 → #53 → #56 → #58 → #62 → #60 → #63`
를 순서대로 `main`에 병합한다.

실무적 제약(관측):

- 14개 PR은 **전부 Draft**이며 서로의 head를 base로 하는 스택이다
  (예: #50 base = `feat/keycli-k09-04a-...`, #60 base = `feat/keycli-k10-05-...`).
- `#48`/`#49`의 base는 `codex/keycli-v09-k09-03` 브랜치다. `main`이 아니다.
- 따라서 각 PR base를 `main`으로 재지정하거나, 앞 PR이 병합될 때마다 GitHub가
  base 브랜치를 따라가며 merge commit을 만든다. **14회의 retarget + merge commit**이
  발생하고, 각 단계마다 CI가 다시 돈다.
- 같은 파일을 후속 PR이 고치는 구간(`version_observe.go`, `finalize.go`,
  `provideridentity_test.go`, `providerlifecycle/cloudflare.go`, 가이드 2건)에서
  단계별 충돌 해소가 필요하다.
- Draft 해제·base 변경은 **소유 브랜치/PR 변경**이므로 이번 권한 범위 밖이다.

### Option B — 신규 Landing PR

원본 owning PR의 blob만 조합한 단일 후보 브랜치를 `main`에서 만들고, 한 번의 PR로 인수한다.

이번 작업에서 실제로 준비했고 **세 가지 방식으로 같은 Tree를 재현**했다.

| 방식 | 산출물 | Tree | 커밋 수 |
|---|---|---|---|
| B1. 단일 합성 커밋 | `landing/keycli-v010-source-20261110` @ `7bbc01d5` | `ee735340` | 1 |
| B2. 선형 replay (권장 payload) | `landing/keycli-v010-source-history-20261110` @ `80bdd82b` | `ee735340` | 22 |
| B3. blob overlay (검산용) | 임시 index, 커밋 없음 | `ee735340` | — |

B2는 각 PR head의 실제 커밋을 `git cherry-pick -x`로 의존성 순서대로 재생한 결과다
(원저자·원메시지·`(cherry picked from ...)` 추적 유지). **충돌 0건**으로 완주했고
최종 Tree가 B1과 동일하다. 두 브랜치 모두 payload 위에 docs 커밋만 추가돼 있다
(payload Tree는 `ee735340` 그대로).

## 기준별 평가

| 기준 | Option A | Option B (B2 payload) |
|---|---|---|
| 원본 HEAD 보존 | 유지(브랜치 그대로) | 유지(원 브랜치 불변, 커밋은 replay로 보존) |
| Git Tree 정합성 | 단계별 merge commit으로 Tree가 계속 변형 | **단일 시점 Tree `ee735340`** 로 고정·검증 가능 |
| 기능 소유권 감사 가능성 | PR 단위로 명확 | manifest + 커밋 원저자·메시지로 명확 |
| GitHub base/branch 충돌 | 14회 retarget, 스택 충돌 위험 | 없음(단일 브랜치, main 직계) |
| 필요한 추가 CI 비용 | 14회 이상 | **1회** (기존 #61 증거는 코드 동등 범위로 재사용) |
| 사용자 승인·되돌리기 | 단계별 되돌리기 쉬움, 승인 14회 | 단일 revert, 승인 1회 |
| 릴리스 이력 명료성 | merge commit 14개, 스택 흔적 | 선형 22커밋, 기능별 메시지 유지 |

## 권고

**Option B + B2 payload**(선형 replay 브랜치 `80bdd82b`, Tree `ee735340`)를 권고한다.

이유:

1. **정합성**: 검증된 Tree를 한 점으로 고정할 수 있고, landing PR의 test merge Tree가
   후보 Tree와 같은지 한 번만 확인하면 된다.
2. **비용**: Hosted CI 1회. 기존 #61 성공 증거는 코드 동등 범위에서 재사용 가능하다.
3. **감사 가능성**: 22개 커밋이 원저자와 `cherry picked from` 참조를 유지하므로
   Option A의 추적 장점을 상당 부분 보존한다.
4. **안전**: Draft PR·소유 브랜치를 닫거나 base를 바꾸지 않는다.

보완(필수):

- Landing PR 본문에 `#47~#63` 매핑과 `#54/#57/#61은 병합 금지`를 명시한다.
- `exact-source-manifest.json`을 PR 본문에 링크해 기능 소유권을 고정한다.
- 병합 후 원 Draft PR은 **닫지 않는다**(별도 승인 필요). 필요하면 "landed via #NN" 코멘트만 남긴다.

## Option A를 택할 경우의 최소 조건

사용자가 커밋 단위 이력을 반드시 PR별로 남겨야 한다면:

1. `#47`만 `main`에 병합(나머지는 여전히 스택).
2. `#48`/`#49` base를 `main`으로 재지정 → 충돌 확인 → CI.
3. 위 순서를 반복하며 매 단계 Tree를 manifest와 대조.
4. `#54/#57/#61`은 절대 병합하지 않고, 병합 후 닫기 여부는 별도 결정.

이 경로는 최소 14회 CI와 14회 승인 단위를 요구하므로, 이번 작업 범위에서는
`CANDIDATE_READY` 상태로 보류하는 것이 맞다.
