---
status: historical
owner: keycli-delivery
last_verified_commit: 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace
---

# Git blob 정합성 보고서

후보 Tree와 검증 전용 Tree를 blob 단위로 비교한 결과다. 모든 값은
`git rev-parse <tree>:<path>`, `git diff-tree -r`, `git ls-tree -r`로 직접 산출했다.
파일별 전체 blob 목록은 [exact-source-manifest.json](exact-source-manifest.json)이 소유한다.

## 요약

| 비교 대상 | 경로 수(대상) | blob 차이 | 성격 |
|---|---|---|---|
| 후보 `ee735340` vs #61 `ac5c3d4a` | 278 | **1** | 문서 1건, 후보가 더 정확 |
| 후보 `ee735340` vs #57 `56bd06c9` | 263 | 16 | #59/#60/#62/#63 미포함 (설계상) |
| 후보 `ee735340` vs #54 `2077e2b7` | 260 | 23 | #55/#59/#58/#60/#62/#63 미포함 |
| 후보 `ee735340` vs main `6568e233` | 218 | 65 | 60 added / 5 modified, 삭제 0 |

`candidate vs main`: `65 files changed, 7437 insertions(+), 35 deletions(-)`.

## 구성 방식 교차 검증

두 가지 독립 구성이 **같은 Tree**를 만들었다.

| 방식 | 절차 | 결과 Tree |
|---|---|---|
| A. patch 적용 | `git diff --binary <base> <head>` × 14 → `git apply --check` → `git apply` | `ee73534082147438211d27d1adddbe25a9c97c5e` |
| B. blob overlay | 별도 index에 `git read-tree main` 후 각 PR 변경 경로를 head blob으로 `git update-index --index-info` | `ee73534082147438211d27d1adddbe25a9c97c5e` |

방식 A는 순서 의존(스택 순서) 오류를, 방식 B는 파일 누락을 각각 다르게 검출한다.
두 결과가 같다는 것은 "14개 PR 고유 변경분의 합집합"이 정확히 재구성됐다는 뜻이다.

적용 순서(방식 A, 14개 전부 `git apply --check` 통과):

```
47 → 48 → 50 → 55 → 59 → 49 → 51 → 52 → 53 → 56 → 58 → 62 → 60 → 63
```

## 유일한 차이: `docs/guides/credential-rotation-lifecycle.md`

```
candidate 449296d245d816639aa0fb3c62212d06c03a510f  (= #56 head blob, K10-04R 절 포함)
#61 tree  13993e9eddc91aea8be70c6d713d2f84778d05d1  (= #53 head blob, K10-04R 절 없음)
```

- `#56/head:docs/guides/credential-rotation-lifecycle.md` = `449296d2`
- `#53/head:...` = `13993e9e`
- `#57/head:...` = `13993e9e` ← **#57 overlay에서 이미 되돌아감**
- `#61/head:...` = `13993e9e`

즉 #57(검증 전용)이 blob overlay 목록을 만들 때 #56의 문서 수정 대신 #53 시점 blob을
사용했고, 그 상태가 #61까지 이어졌다. 차이는 **삭제 14줄, 추가 0줄, 코드 0줄**이다.

```diff
-## K10-04R: 공급자별 이전 세대 Identity 재검증 (후속 Draft)
-... (Provider별 대상/금지 혼동 표, 회귀 범위, NOT VERIFIED 문구)
```

판정: 후보가 **소유 PR 기준으로 더 정확**하고, #61 Tree는 문서 회귀를 포함한다.
코드·테스트·`go.mod`·`go.sum`·정책/잠금 파일·릴리스 메타데이터는 **완전히 동일**하다.

## 코드 동등성 확인 (증거 재사용의 근거)

- 후보와 #61의 차이 경로 중 `docs/` 밖 경로: **0개**
  (`git diff-tree -r --no-commit-id --name-only <A> <B> | grep -v '^docs/' | wc -l` → `0`)
- `go.mod`, `go.sum`: 양쪽 동일 blob
- `verify.sh`, `.github/workflows/*`: 양쪽 동일 blob
- 테스트 fixture 경로: 양쪽 동일 blob (#62 반영 완료)

따라서 **컴파일·테스트 동작은 동일**하고, 차이는 `verify.sh --suite docs`가 검사하는
"파일 존재/스크립트 문법" 범위에도 영향을 주지 않는다. 그럼에도 지시에 따라
`EXACT_SOURCE_MATCH`는 **false**로 기록하고, CI 재사용은 "코드 동등 범위"로 한정한다
([ci-evidence-reuse.md](ci-evidence-reuse.md)).

## main 편입 후 재검증

main이 전진해도 후보 blob 동일성을 확인할 수 있도록 manifest는 경로별 blob SHA를
고정한다. 편입 후:

```bash
bash docs/reviews/2026-11-10-v010-source-landing/verify-source-parity.sh
```

스크립트는 `exact-source-manifest.json`의 75개 경로를 현재 HEAD와 대조하고,
불일치 경로를 `path expected actual` 형식으로 출력한다.
