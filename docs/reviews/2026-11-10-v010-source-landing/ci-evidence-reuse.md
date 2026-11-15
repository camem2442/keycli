---
status: historical
owner: keycli-delivery
last_verified_commit: 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace
---

# CI 증거 재사용 판정

## 실측: Run 42787409731

| 항목 | 관측값 |
|---|---|
| Repository / Workflow | `camem2442/keycli` · `test` (`.github/workflows/test.yml`) |
| Run / Job | `42787409731` · `linux` (유일한 job) |
| Event / PR | `pull_request` · #61 |
| 결론 | `success` (2026-11-10T09:02:51Z → 09:04:10Z) |
| Checkout | `HEAD is now at f8ab4d3 Merge 2ce3ccb0 into 60dca535` |
| 실제 checkout Tree | `ac5c3d4a582d800ebb0996545df4a5ddacb3847b` |
| Go | `go1.22.12 linux/amd64` (`setup-go@v5`, `go-version-file: go.mod`, spec `1.22`) |
| Runner | `ubuntu-latest` (setup-go cache key `ubuntu26`) |
| 명령 | `./verify.sh --suite all` (`CGO_ENABLED=0`) |
| 결과 카운터 | `ok` 패키지 **37개**, 실패 0 |
| mac-smoke | `NOT VERIFIED on Linux — skipped` |
| 로그 종료 | `verify.sh: suite=all ok` |

`f8ab4d3`은 `2ce3ccb0`을 `60dca535`에 병합한 GitHub test merge commit이며,
그 Tree가 head Tree와 같다(`ac5c3d4a`). 따라서 "CI가 검증한 소스"는 #61 head 그 자체다.

## 6개 축 독립 판정

| 축 | 판정 | 근거 |
|---|---|---|
| `EXACT_SOURCE_MATCH` | **false** | 후보 `ee735340` vs `ac5c3d4a`: 278경로 중 1개 blob 차이(`docs/guides/credential-rotation-lifecycle.md`) |
| `SUITE_MATCH` | **true** | 같은 `verify.sh` blob, 같은 `--suite all`, 같은 `CGO_ENABLED=0` |
| `TOOLCHAIN_MATCH` | **true** | Go 1.22.12 (CI linux/amd64, 로컬 darwin/arm64) |
| `ENVIRONMENT_MATCH` | **false** | CI `linux/amd64 ubuntu`, 로컬 `darwin/arm64` |
| `BASE_AND_MERGE_TREE_MATCH` | **true (구조상)** | 후보는 `main 4c972d98`의 직계 후손 → landing PR의 test merge Tree = 후보 Tree. 실제 PR 생성 전이므로 **PR 생성 후 재확인 필요** |
| `REQUIRED_CHECKS_SATISFIED` | **true (정책 수준)** | `main` 브랜치 보호·ruleset 없음(`404 Branch not protected`, `rulesets: []`). 즉 "Actions green"은 GitHub 강제가 아니라 저장소 관례 |

## 재사용 결정

`EXACT_SOURCE_MATCH=false`이므로 **무조건 재사용하지 않는다**. 대신 범위를 명시해 재사용한다.

- 재사용 근거: 코드 경로 차이 0. `docs/` 밖 불일치 0건, `go.mod`/`go.sum`/`verify.sh`/
  워크플로/테스트 fixture 모두 동일 blob. 차이는 삭제 14줄짜리 문서 1건이며 후보가 더 정확하다.
- 재사용 한계: `docs/guides/credential-rotation-lifecycle.md`의 K10-04R 절은
  **CI가 검증한 blob이 아니다**. 문서 내용 검증(링크·사실관계)은 별도 범위다.
- 결론: `INTEGRATION_CI = REUSED_VERIFIED (코드 동등 범위)`. 새 Hosted CI 재실행은
  **권고하지 않는다**(동일 코드에 비용 중복). landing PR을 실제로 열 때 GitHub가
  test merge commit을 만들면 그 Tree만 후보 Tree와 대조하면 된다.

`workspace-infra#239`(pre-dispatch 중복 분류기)는 Draft/advisory이므로 이 판정을
승인이나 필수 검사 PASS로 승격하지 않는다.

## 로컬 보강 검증 (격리 worktree `landing/keycli-v010-source-20261110`)

환경: macOS `darwin/arm64`, Go **1.22.12** (`GOTOOLCHAIN=go1.22.12`, module cache toolchain),
`CGO_ENABLED=0`, `TMPDIR`을 worktree 내부 `.tmp`(0700, 비심볼릭)로 지정.

| 단계 | 명령 | 결과 |
|---|---|---|
| docs | `bash verify.sh --suite docs` | **ok** |
| policy | `bash verify.sh --suite policy` | **ok** (시크릿 패턴 0) |
| gofmt | `gofmt -l .` (1.22.12) | **ok** (출력 없음) |
| vet darwin | `GOOS=darwin GOARCH={arm64,amd64} go vet ./...` | **ok** |
| vet linux | `GOOS=linux GOARCH=amd64 go vet ./...` | **ok** |
| unit | `go test -tags=kc_testhooks ./...` | 36 pkg ok, 1 pkg 환경조건 실패(아래) |
| release | `bash verify.sh --suite release` | 산출물 생성 ok / MCP 실행 단계 **NOT_VERIFIED** (macOS에서 Linux 바이너리 실행 불가, exit 126) |
| race (보조) | `go test -race ./internal/delivery/... ./internal/rotation/... ./internal/identity/githubwif/... ./internal/target/{gh,cloudflare}/... ./internal/cli/...` | **ok** (Go 1.27.3, 아래 참조) |
| mac-smoke | — | **NOT VERIFIED (의도적 미실행)** |

### 환경 조건 실패 2건 (소스 회귀 아님)

1. `internal/rotation` 7개 테스트 — 기본 `$TMPDIR`(`/var/folders/...`)에서
   `open delivery journal parent: not a directory`.
   원인: journal 저장소가 경로를 `O_NOFOLLOW|O_DIRECTORY`로 순회하는데 macOS의
   `/var` → `/private/var` 심볼릭 링크에서 `ENOTDIR`. `TMPDIR`을 비심볼릭 경로로
   바꾸면 전부 통과한다. Linux CI에는 해당 심볼릭 링크가 없어 재현되지 않는다.
2. `internal/cursor.TestWireCursorVertex` — `Cursor is running: refusing to modify its
   state database while active`. 실제 Cursor 프로세스가 떠 있어 가드가 정상 동작한 결과다.
   같은 테스트를 `main 4c972d98`에서 실행해 **동일 실패**를 재현했으므로 후보 회귀가 아니다.

두 항목 모두 후속 정리 대상으로 [next-agent-handoff.md](next-agent-handoff.md)에 남겼다.

### race 도구 체인 주의

`go1.22.12` + `-race`는 이 macOS에서 컴파일은 되지만 실행 시
`dyld: missing LC_UUID load command` / `signal: abort trap`으로 중단된다(도구·OS 호환).
동일 소스를 `go1.27.3`으로 `-race` 실행하면 변경 패키지 전부 통과한다. 이는
**보조 신호**이며 Go 1.22.12 게이트를 대체하지 않는다. CI도 `-race`를 실행하지 않는다.

## 안전 확인

- 실제 provider mutation **0회**: 모든 provider 테스트는 주입된 `http.RoundTripper`/
  합성 transport를 쓴다(`httptest` 서버·실 도메인 호출 0).
- 실제 Keychain 미변경: `internal/backend/...`(keychain 포함) 실행 전후
  `~/Library/Keychains/login.keychain-db` mtime 변화 0.
- `~/.kc` 미변경(mtime 2026-11-08 유지), `KC_KEY` 미설정, 설치된 `kc`(v0.8.0) 미변경.
