---
status: historical
owner: keycli-delivery
last_verified_commit: 6a44a1344462ef4c80cd62ab4226a347c8b15d83
---

# Landing PR 인계 (Phase 1)

## 판정: 게시 HOLD — 권한 없음

`git_push`와 `open_pr`은 `goal_authority_core.EXPLICIT_ONLY`에 정의된 효과지만,
이 작업에 대해 발급된 Grant가 존재하지 않는다. **스스로 Grant를 만들지 않는다**는
규칙에 따라 원격 게시를 수행하지 않았다.

| 근거 | 관측값 |
|---|---|
| 등록 Goal (`_management/state/goal-runtime/registry.json`) | 3개: `M4.5-F-STUDY-APPS-20261024`, `M4.5-F-AI-CORE-GAC-20261024`, `M4.5-F-WI-VERIFY-SLOTS-20261024` — **KeyCLI landing Goal 없음** |
| Grant 파일 (`state/goal-runtime/authorizations/*.json`) | 3개 모두 effects가 `local_edit`, `local_test`, `git_commit_local` 계열 — **`git_push`/`open_pr` 0건** |
| 활성 Writer Claim (`claims.db`) | 3 ACTIVE (`study-apps`, `ai-core`, `workspace-infra` verify-slots) — KeyCLI claim 없음 |
| 동일 목적 원격 PR | 없음 (open PR 23개 중 landing 브랜치 0개) |
| 원격 `main` | `4c972d98` — 기준값과 동일 |

## 게시 준비 상태 (권한만 있으면 즉시 실행 가능)

| 항목 | 값 |
|---|---|
| payload 브랜치 | `landing/keycli-v010-source-history-20261110` (= `80bdd82b`, 22 커밋) |
| payload Tree | `ee73534082147438211d27d1adddbe25a9c97c5e` |
| 비교용 후보 | `landing/keycli-v010-source-20261110` (= `7bbc01d`, 단일 커밋, 같은 Tree) |
| base | `main` @ `4c972d98` |
| 변경 규모 | 65 files, +7437/−35 |
| 게시 명령(미실행) | `git push -u origin landing/keycli-v010-source-history-20261110` → `gh pr create --draft --base main --head landing/keycli-v010-source-history-20261110` |

## PR 본문 초안 (게시 승인 시 사용)

```markdown
## Source landing: KeyCLI v0.9/v0.10 (Draft)

Composes only the owning Draft PR heads, in dependency order:
#47 → #48 → #50 → #55 → #59 → #49 → #51 → #52 → #53 → #56 → #58 → #62 → #60 → #63
(22 commits preserved with original authors and `cherry picked from` trailers)

- Tree: ee73534082147438211d27d1adddbe25a9c97c5e (reproduced by 3 independent methods)
- Base: main 4c972d98
- Verification-only PRs #54 / #57 / #61 are NOT merged and not part of this history.
- Blob manifest: docs/reviews/2026-11-10-v010-source-landing/exact-source-manifest.json
- Parity vs #61 tree ac5c3d4a: 277/278 identical; the single difference is
  docs/guides/credential-rotation-lifecycle.md, where #61 lost #56's K10-04R
  section. This PR keeps #56's version.
- CI evidence reuse: run 42787409731 (Go 1.22.12 linux/amd64, `verify.sh --suite all`)
  applies to code parity only; this PR's own test-merge tree must be checked.
- Provider live acceptance: NOT_VERIFIED (no real provider mutation was performed).
```

## 게시 후 해야 할 일 (승인 시)

1. `gh pr checks --watch`로 자동 생성된 `test / linux` **1회**만 관측한다(수동 재실행 금지).
2. test merge commit/Tree를 후보 Tree와 대조한다. base가 `4c972d98`이면 fast-forward 가능
   상태이므로 merge Tree = `ee735340`이어야 한다.
3. 결과를 [landing-ci-evidence.md](landing-ci-evidence.md)에 덧붙인다.

## 금지 유지

- `#54`, `#57`, `#61` 병합 금지.
- 기존 Draft PR base 변경·닫기 금지.
- force push 금지.
