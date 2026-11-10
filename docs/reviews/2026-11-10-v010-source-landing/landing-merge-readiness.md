---
status: historical
owner: keycli-delivery
last_verified_commit: 6a44a1344462ef4c80cd62ab4226a347c8b15d83
---

# 병합 준비도 (Phase 1)

## 조건별 상태

| 조건 | 요구 | 현재 | 근거 |
|---|---|---|---|
| `SOURCE_PARITY` | PASS | **PASS** | 65/65 경로 일치, 소유 PR 14개 최종 blob 전부 포함 |
| `EXACT_HEAD_CI` | PASS | **NOT_VERIFIED** | landing PR 없음(게시 권한 없음) |
| `REQUIRED_CHECKS` | PASS | **NOT_APPLICABLE** | `main` 브랜치 보호·ruleset 없음(`404 Branch not protected`, `rulesets: []`) |
| `BASE_CURRENT` | PASS | **PASS** | `origin/main` = `4c972d98` = 기준값, 재조회 시점에도 동일 |
| `WRITER_AUTHORITY` | PASS | **BLOCKED** | 등록 Goal·Claim 없음 |
| `MERGE_AUTHORITY` | PASS | **BLOCKED** | `merge` 효과를 가진 Grant 없음 |

```
MERGE_HOLD
```

사유는 하나다: **이 작업에 대한 Goal/Grant가 등록돼 있지 않다.** 병합 권한이 생기면
즉시 실행할 수 있도록 소스·증거는 준비돼 있다.

## 병합 시 실행 순서 (승인 후)

1. 최신 `origin/main` 재확인 → 기준이 바뀌었으면 후보를 rebase하지 말고 새 격리 후보를 만든다.
2. landing PR HEAD == `80bdd82b`(또는 이후 추가 커밋) 확인, test merge Tree == `ee735340` 확인.
3. `gh pr checks` 결과 `test / linux` success 확인.
4. `git merge --no-ff` 또는 `--squash` 선택:
   - 권장: **merge commit 없이 fast-forward가 가능하므로 rebase-merge 또는 merge commit** 중
     저장소 관례를 따른다. 22커밋 보존을 위해 merge commit을 권장.
5. 병합 후 `main` Tree 확인:
   ```bash
   git fetch origin main
   git rev-parse origin/main^{tree}   # 기대: ee73534082147438211d27d1adddbe25a9c97c5e
   bash docs/reviews/2026-11-10-v010-source-landing/verify-source-parity.sh origin/main
   ```
6. 기능 PR `#47~#63`은 **닫지 않는다**. `landed via #NN` 코멘트만 남긴다(별도 승인).

## 병합 후 즉시 확인할 것

| 항목 | 기대 |
|---|---|
| `main` Tree | `ee735340` (문서 커밋이 함께 병합되면 landing 디렉터리만 추가된 상위 Tree) |
| `verify.sh --suite docs` | ok |
| `kc delivery plan`, `kc delivery status`, `kc auth probe` | 신규 명령 노출 |
| `internal/rotation` | CLI 미노출 유지 (v0.10 CLI 배선 전) |

## 롤백

단일 revert로 되돌린다. 선형 replay 이력을 payload로 썼기 때문에 revert commit 1개로
소스 상태가 `4c972d98`로 복귀한다. 태그·릴리스는 아직 없으므로 함께 되돌릴 대상이 없다.
