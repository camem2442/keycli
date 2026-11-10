---
status: historical
owner: keycli-delivery
last_verified_commit: 6a44a1344462ef4c80cd62ab4226a347c8b15d83
---

# Landing CI 증거 (Phase 1)

## 현재 값

```
LANDING_EXACT_CI=NOT_VERIFIED
```

이유: 게시 권한이 없어 **landing PR이 존재하지 않는다**(권한 판정은
[landing-pr-handoff.md](landing-pr-handoff.md)). 자동 workflow를 관측할 대상이 없다.

## 재사용 중인 기존 증거 (변경 없음)

| 항목 | 값 |
|---|---|
| Run | `42787409731` (PR #61, `pull_request`) |
| 결론 | `success` |
| checkout | `f8ab4d3` = `2ce3ccb0` into `60dca535` |
| Tree | `ac5c3d4a582d800ebb0996545df4a5ddacb3847b` |
| Go | `go1.22.12 linux/amd64` |
| 명령 | `./verify.sh --suite all` (`CGO_ENABLED=0`) |
| 결과 | 37 패키지 `ok`, mac-smoke `NOT VERIFIED (Linux)` |

판정 축([ci-evidence-reuse.md](ci-evidence-reuse.md)):

| 축 | 값 |
|---|---|
| `EXACT_SOURCE_MATCH` | false (문서 1건) |
| `SUITE_MATCH` | true |
| `TOOLCHAIN_MATCH` | true |
| `ENVIRONMENT_MATCH` | false (linux vs darwin) |
| `BASE_AND_MERGE_TREE_MATCH` | 미확인 (PR 없음) |
| `REQUIRED_CHECKS_SATISFIED` | 정책 수준 (main 보호 없음) |

## 게시 시 기록할 체크리스트

| 확인 항목 | 기록 방법 | 현재 |
|---|---|---|
| exact PR HEAD | `gh pr view --json headRefOid` | 없음 |
| test merge commit | `gh pr view --json potentialMergeCommit` 또는 `refs/pull/N/merge` | 없음 |
| test merge Tree | `git rev-parse <merge>^{tree}` == `ee735340` | 없음 |
| Go version | job log `go version` | 없음 |
| runner OS/arch | job log / cache key | 없음 |
| `verify.sh --suite all` | job log 마지막 줄 | 없음 |
| job conclusion | `gh pr checks` | 없음 |
| required checks | 저장소 정책(보호 없음) | 해당 없음 |

## 재현한 로컬 증거 (게시와 무관, 유효)

| 단계 | 결과 |
|---|---|
| `verify.sh --suite docs` | ok |
| `verify.sh --suite policy` | ok |
| `verify-source-parity.sh` | 65 경로, mismatch 0 |
| Go 1.22.12 unit | 36 패키지 ok, `internal/cursor` 1건은 로컬 Cursor 실행 중 가드(기준 `main`에서 동일 재현) |
| Go 1.22.12 release | kc 4타깃 + keycli-linux-amd64 + SHA256SUMS 생성 ok |

## 추가 수동 CI 금지

게시가 이뤄지면 자동 생성되는 `test / linux` **1회**만 사용한다. 중복 비용을 만들지 않는다.
실패 시 원인 파일만 격리 worktree에서 수정하고, 새 HEAD에 증거를 다시 결속한다.
