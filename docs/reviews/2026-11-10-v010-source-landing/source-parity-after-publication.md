---
status: historical
owner: keycli-delivery
last_verified_commit: 6a44a1344462ef4c80cd62ab4226a347c8b15d83
---

# 게시 전후 소스 정합성

## 이번 세션 재검증 (게시 전, 2026-11-10)

| 검사 | 명령 | 결과 |
|---|---|---|
| 기준선 | `git rev-parse origin/main` | `4c972d98…` (고정값과 동일) |
| 후보 payload | `git rev-parse 80bdd82b^{tree}` / `7bbc01d^{tree}` | 둘 다 `ee735340…` |
| 브랜치 tip | `landing/*` 2개 | 문서 커밋 포함 tip, payload Tree 동일 |
| 65경로 blob | `verify-source-parity.sh` | `paths_checked=65 mismatches=0` |
| PR head 드리프트 | manifest 대비 #47~#63 head SHA | 0건 |
| 검증 전용 head | #54 `76fb6afd`, #57 `60dca535`, #61 `2ce3ccb0` | 변동 없음 |
| 소스 충돌 | `git apply --check` × 14 | 0건 |
| 기존 worktree | 원본 checkout·다른 세션 슬롯 | 변경 없음 |

## 게시 후 재검증 절차

게시가 승인되면 다음을 **그때의 관측값**으로 다시 수행한다(이 문서의 값을 복사하지 않는다).

```bash
git fetch origin main
git rev-parse origin/main                 # base가 바뀌었는지 먼저 확인
git rev-parse <pr-merge-ref>^{tree}       # 기대: ee73534082147438211d27d1adddbe25a9c97c5e
bash docs/reviews/2026-11-10-v010-source-landing/verify-source-parity.sh <pr-merge-ref>
```

병합 후:

```bash
git rev-parse origin/main^{tree}
bash docs/reviews/2026-11-10-v010-source-landing/verify-source-parity.sh origin/main
```

기대: 65경로 mismatch 0. `main` Tree는 문서 커밋이 함께 병합된 경우 landing 디렉터리
파일들 때문에 `ee735340`보다 상위 Tree가 되지만, **소스 65경로는 그대로**여야 한다.

## base가 전진한 경우

`origin/main`이 `4c972d98`에서 이동했다면:

1. 이동 커밋의 변경 경로를 확인한다(`git diff --name-only 4c972d98..origin/main`).
2. 후보 65경로와 겹치는 경로가 있으면 **자동 rebase 금지**. 격리 worktree에서
   충돌 파일·원본 blob·후보 blob을 기록하고 별도 판단을 남긴다.
3. 겹치지 않으면 새 base에서 replay를 다시 만들어 Tree가 `ee735340`과 같은지 확인한다
   (같은 blob 집합이면 Tree도 같다).
4. 어떤 경우에도 기존 `landing/*` 브랜치를 force push하지 않는다.

## 현재 상태

```
SOURCE_PARITY (게시 전)     = PASS
SOURCE_PARITY (게시 후)     = NOT_APPLICABLE (게시 이벤트 없음)
```
