---
status: current
owner: keycli-delivery
last_verified_commit: 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace
---

# 다음 실행자 인계 (2026-11-10)

## 30초 복원

- 기준 `origin/main = 4c972d98` (tree `6568e233`). 최신 릴리스·설치 pin은 **v0.8.0**.
- 후보 소스: `landing/keycli-v010-source-20261110` @ `7bbc01d5` (Tree `ee735340`, 278 paths).
  동일 Tree의 선형 replay: `landing/keycli-v010-source-history-20261110` @ `80bdd82b` (22 commits).
- 근거 문서: `docs/reviews/2026-11-10-v010-source-landing/` (README에서 전체 지도).
- 상태: `MAIN_LANDING = CANDIDATE_READY`, `MERGE_HOLD`, `RELEASE = BLOCKED`.

## 완료된 것

1. `origin/main`이 고정 기준값 `4c972d98`과 동일함을 확인.
2. #46~#63 상태·base/head/tree/blob 수집(`exact-source-manifest.json`, 75경로).
3. 소유 PR 14개만으로 후보 Tree 재구성 — **세 방식 모두 `ee735340`**:
   patch 적용, blob overlay, 22커밋 cherry-pick replay.
4. #61 CI Run `42787409731` 실측: Go 1.22.12 linux/amd64, `verify.sh --suite all`,
   checkout `f8ab4d3`, Tree `ac5c3d4a`, 37 pkg ok, mac-smoke skip.
5. 후보 vs #61 blob 대조: 277/278 동일, 문서 1건 차이(후보가 정확).
6. 로컬 검증(격리 worktree, Go 1.22.12): docs·policy·gofmt·vet(darwin/linux)·release ok,
   unit 36 pkg ok + 1 환경조건 실패, 변경 패키지 `-race` 보조 통과.
7. 병합 전략 A/B 비교 및 권고(Option B + replay payload).

## 블로커 (효과별로 분리)

| 블로커 | 종류 | 해소 조건 |
|---|---|---|
| landing PR 생성·병합 | **사용자 결정 필요** (게시 권한 없음) | 브랜치 push + PR 생성 + 병합 승인 |
| Draft PR #47~#63 처리 | **사용자 결정 필요** | 병합 후에도 닫지 않음 권고. 닫으려면 별도 승인 |
| 릴리스 태그·GitHub Release | **사용자 결정 필요** | v0.9.0 범위 확정 + 태그 권한 |
| mac-smoke | **증거 없음** | 실 Mac 1회 실행(throwaway `kc.test.*`) |
| `TRUSTED_HOST` | **정본 도구 미구현** | WI-KC-02 구현 (workspace-infra) |
| 실 provider 인수 | **외부 권한·예산** | 계정·범위·예산 명시 승인 |

## 다음 승인 대상 (한 번에 결정할 것)

1. landing 브랜치 push + PR 생성 → CI 1회 → 병합 (Option B2 payload `80bdd82b` 권장).
2. 병합 후 v0.9.0 태그 여부, 릴리스 노트 초안 승인.
3. Draft PR을 열어둘지 / "landed via #NN" 코멘트만 남길지.

## 하지 말 것

- `#54`, `#57`, `#61` 병합. 내용은 후보에 모두 포함돼 있고, 이들은 검증 전용이다.
- #61 Tree(`ac5c3d4a`)를 그대로 후보로 쓰기 — #56 문서 1건이 빠져 있다.
- `#40`이 체크아웃된 공유 worktree(저장소 루트의 기존 checkout)에서
  reset/clean/checkout. 그 브랜치는 다른 작업의 활성 상태다.

## 남은 정리 항목 (후속, 이번 범위 밖)

| 항목 | 근거 | 제안 |
|---|---|---|
| macOS `$TMPDIR` 심볼릭 링크에서 journal 경로 순회 실패 | `internal/rotation` 7개 테스트가 `/var → /private/var`에서 `ENOTDIR` | 테스트에서 `t.TempDir()` 대신 비심볼릭 경로를 쓰거나 `os.TempDir()` 정규화 |
| `TestWireCursorVertex`가 로컬 Cursor 실행 중이면 실패 | `main 4c972d98`에서도 동일 재현 | 테스트에서 실행 감지를 주입 가능하게 변경 |
| `go1.22.12 -race`가 이 macOS에서 abort | `dyld: missing LC_UUID` | CI에 `-race` 잡 추가 또는 최신 Go로 보조 실행 |
| #61 overlay가 #56 문서 blob을 되돌림 | `git-blob-parity-report.md` | 재사용 시 문서 blob 수동 확인 |

## workspace-infra 연결 (구현 후속 분리)

KeyCLI 내부에 새 Goal 승인 엔진을 만들지 않는다. 정본은
`tooling/goal_authority_core.py`, `goal_mutation.py`, `goal_verification.py`,
`goal_delivery_loop.py`이며, 이번 작업은 **읽기만** 했다
(`agent-harness/INTEGRATION.md`는 현재 `main`에 없다 → 경로 확인 필요).

| ID | 범위 | 입력 | 출력 |
|---|---|---|---|
| WI-KC-01 | Read-only receipt projection | KeyCLI journal/receipt → Goal 상태 | 조회 전용 투영, 승인 생성 없음 |
| WI-KC-02 | Trusted Host Admission + Writer/Grant validation | HostAuthorize 신뢰 앵커, claim generation | provider effect 실행 전 fail-closed |
| WI-KC-03 | Issuer/Revoker separation + durable reconciliation | #60 lifecycle, journal `UNKNOWN_EFFECT` | 발급/폐기 분리와 영속 대조 |

운영 효과 권한과 소스 검증 결과는 독립으로 유지한다. #230(Cloudflare Auth Cutover),
#239(CI 중복 분류기), #241(Hook H1 replay), #251(KeyCLI delivery consumer design)은
모두 **Draft/OPEN**이며 이번 작업에서 변경하지 않았다. standard-vm 작업도 침범하지 않았다.

## 검증 재현 명령

```bash
# 후보 Tree
git rev-parse 7bbc01d53^{tree}         # ee735340...
git rev-parse 80bdd82b5^{tree}         # ee735340...

# blob 재검증 (main 편입 후에도)
bash docs/reviews/2026-11-10-v010-source-landing/verify-source-parity.sh

# 로컬 게이트 (Go 1.22.12, 격리 worktree, 비심볼릭 TMPDIR)
GR=$(GOTOOLCHAIN=go1.22.12 go env GOROOT)
PATH="$GR/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0 TMPDIR="$PWD/.tmp" \
  bash verify.sh --suite docs && \
PATH="$GR/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0 TMPDIR="$PWD/.tmp" \
  bash verify.sh --suite policy && \
PATH="$GR/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=0 TMPDIR="$PWD/.tmp" \
  bash verify.sh --suite unit   # internal/cursor 1건은 로컬 Cursor 종료 후 재확인
```

`--suite all`을 Mac에서 그대로 돌리면 `mac-smoke`가 **실제 키체인**을 사용한다.
격리 작업에서는 `all` 대신 위 3개 suite + `release`를 사용한다.

## 2026-11-10 후속 직렬 실행 결과 (Phase 1–3)

| 단계 | 결과 | 위치 |
|---|---|---|
| Phase 0 기준선 | PASS — `origin/main` `4c972d98`, 후보 Tree `ee735340`, 65/65 blob, PR head 드리프트 0 | 이 디렉터리 |
| Phase 1 게시 | **HOLD** — 등록 Goal/Grant 없음(`git_push`/`open_pr` 0건). 산출물 4종 기록 | [landing-pr-handoff.md](landing-pr-handoff.md) |
| Phase 2 WI-KC-01 | **구현·테스트 완료 (source-only)** — `tooling/keycli_delivery_receipt.py`, 22 테스트 | workspace-infra `opencode/keycli-delivery-receipt-20261110` @ `e82f98d` |
| Phase 3 WI-KC-02 | **구현·테스트 완료 (source-only)** — `tooling/keycli_host_admission.py` (19), `internal/hostbridge` (15) | 같은 workspace-infra 브랜치 + keycli `opencode/kc-hostbridge-20261110` @ `5edac37` |

후속 실행자는 다음을 먼저 확인한다.

- workspace-infra `e82f98d`는 `main`에 병합되지 않았다(로컬 전용).
- keycli `5edac37`은 landing payload `80bdd82b` 위에 있다(`depends_on`).
- 실제 provider mutation·Keychain·`~/.kc`·설치 바이너리 변경은 여전히 0회다.
- `TRUSTED_HOST`는 `SOURCE_PARTIAL` 유지: 호스트 프로세스가 자격증명을 실제로 격리해야
  운영 인수가 성립한다.
