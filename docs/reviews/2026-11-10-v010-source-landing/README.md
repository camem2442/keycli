---
status: historical
owner: keycli-delivery
last_verified_commit: 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace
---

# KeyCLI v0.9.x → v0.10.0 소스 랜딩·증거 대조·릴리스 준비 (2026-11-10)

이 디렉터리는 요청된 `keycli-landing/` 산출물 집합이다. 저장소 문서 정책상 날짜별 근거는
`docs/reviews/`가 소유하므로, 같은 10개 문서를 이 경로에 두었다.

## 한 줄 결론

소유 PR 14개의 고유 변경분만으로 PR #61 검증 Tree와 **코드 수준에서 완전히 동일하고
문서 1개 파일에서만 더 정확한** 후보 Tree `ee73534082147438211d27d1adddbe25a9c97c5e`
(commit `7bbc01d5`)를 로컬 격리 worktree에서 재구성했다. 원격 게시·병합·릴리스는
권한 밖이므로 `MERGE_HOLD`다.

## 최종 판정

| 항목 | 판정 | 근거 |
|---|---|---|
| `SOURCE_PARITY` | **PASS** (문서 1건 drift 명시) | 후보 Tree vs #61 Tree: 278개 경로 중 277개 blob 동일, `docs/guides/credential-rotation-lifecycle.md`만 차이 |
| `INTEGRATION_CI` | **REUSED_VERIFIED** (코드 동등 범위) + 로컬 보강 | CI Run 42787409731 = Go 1.22.12 linux/amd64, `./verify.sh --suite all`, tree `ac5c3d4a` |
| `MAIN_LANDING` | **CANDIDATE_READY** / 병합은 `MERGE_HOLD` | 후보 commit·manifest 준비 완료, 게시 권한 없음 |
| `TRUSTED_HOST` | **SOURCE_PARTIAL** | workspace-infra 정본은 읽기 전용 확인, Host Admission 구현 없음 |
| `PROVIDER_LIVE` | **NOT_VERIFIED** | 실제 provider mutation 0회 (합성 transport만) |
| `RELEASE` | **BLOCKED** | 병합·태그·릴리스 권한 없음, mac-smoke 미실행 |

## 문서 지도

| 문서 | 소유 내용 |
|---|---|
| [pr-dependency-matrix.md](pr-dependency-matrix.md) | PR별 base/head/tree, 변경 파일, 중복·충돌, 누락 수정 |
| [exact-source-manifest.json](exact-source-manifest.json) | 기계 판독용 후보 manifest (파일별 blob SHA 포함) |
| [git-blob-parity-report.md](git-blob-parity-report.md) | 후보 vs #61 blob 단위 대조, 두 구성 방식 교차 검증 |
| [ci-evidence-reuse.md](ci-evidence-reuse.md) | Run 42787409731 실측, 6개 재사용 축 판정, 로컬 실행 결과 |
| [candidate-tree-comparison.md](candidate-tree-comparison.md) | #47/#54/#57/#61/후보 Tree 계층 비교 |
| [merge-strategy-decision.md](merge-strategy-decision.md) | Option A(순차 병합) vs Option B(Landing PR) 권고 |
| [provider-security-gates.md](provider-security-gates.md) | 필수 fail-closed 15항목 ↔ 코드·테스트 매핑 |
| [release-v09-v010-plan.md](release-v09-v010-plan.md) | v0.9.x / v0.10.0 릴리스 순서와 게이트 |
| [next-agent-handoff.md](next-agent-handoff.md) | 다음 실행자용 30초 복원·블로커·승인 대상 |
| [verify-source-parity.sh](verify-source-parity.sh) | main 편입 후 후보 blob 재검증 스크립트 |

## 재현

로컬 브랜치(원격 push 없음):

| 브랜치 | payload commit | payload Tree |
|---|---|---|
| `landing/keycli-v010-source-20261110` | `7bbc01d` (단일 합성 커밋) | `ee735340` |
| `landing/keycli-v010-source-history-20261110` | `80bdd82b` (22커밋 replay) | `ee735340` |

두 브랜치 모두 payload 위에 docs 커밋이 하나 더 얹혀 있다(문서 수정 시 해시가 바뀌므로
여기 고정하지 않는다). payload Tree는 항상 `ee735340`이다.

```bash
# 후보 commit이 만든 Tree
git rev-parse 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace^{tree}
# 기대값: ee73534082147438211d27d1adddbe25a9c97c5e

# manifest 대조 (main 편입 후에도 사용)
bash docs/reviews/2026-11-10-v010-source-landing/verify-source-parity.sh
```

## 경계

- 검증 전용 PR **#54 / #57 / #61은 병합 대상이 아니다.**
- 원격 게시, PR 생성, 병합, 태그, 릴리스, Cloudflare Worker 변경, OCI VM 작업,
  운영 계정 API 키 발급·폐기는 이번 작업에서 **0회** 수행했다.
- 실제 Keychain, `~/.kc`, `KC_KEY`, 설치된 `kc`(`~/.local/bin/kc`, v0.8.0)는 읽기만 했고
  변경하지 않았다.
