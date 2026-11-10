---
status: historical
owner: keycli-delivery
last_verified_commit: 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace
---

# PR 의존성 행렬 (#47–#63)

기준: 2026-11-10 KST, `origin/main = 4c972d9815cf0f89f52dbcd91a772376c8c297dd`
(고정 기준값과 동일 확인). 모든 값은 `git cat-file` / `git ls-tree` /
`git diff-tree`로 직접 조회했고 GitHub API(`refs/pull/*/head`, `refs/pull/*/merge`)와
교차 확인했다.

## 방향성 DAG (관측값으로 확인)

```
main 4c972d98
 └─ #47 e61beed4 (K09-01~03)
     ├─ #48 1edf3cc5 (K09-04A)
     │   └─ #50 378b59e6 (K09-04B)
     │       └─ #55 87bc3a34 (Version tag)
     │           └─ #59 e6c9d0fb (Free gate)
     └─ #49 ffc83ea0 (K10-01)
         └─ #51 8dce0c6d (K10-02)
             └─ #52 82269ad4 (K10-03)
                 └─ #53 7e6da52f (K10-04)
                     └─ #56 54b087e3 (K10-04 fix)
                         └─ #58 d9255689 (K10-05)
                             ├─ #62 dd77ef65 (K10-05 test fix)
                             └─ #60 010d4d37 (K10-06)
                                 └─ #63 18ff271c (K10-06 fix)
```

검증 전용(구현 소유 아님, **DO NOT MERGE**):

```
#54 76fb6afd  (base #47, tree 2077e2b7) — K09-04B + K10-04 join + Cloudflare REST 수리
#57 60dca535  (base #54, tree 56bd06c9) — #54 + #55 + #56 blob overlay
#61 2ce3ccb0  (base #57, tree ac5c3d4a) — #57 + #59 + #60 blob overlay + #62/#63 반영
```

## 소유 PR 표

`Tree`는 각 PR head commit의 Tree SHA다. `변경`은 base..head 고유 변경 경로 수다.
`검증`은 해당 소스가 실제로 어떤 실행에 포함됐는지다.

| PR | Goal | base SHA | head SHA | Tree | 변경 | 검증 실행 |
|---|---|---|---|---|---|---|
| #47 | K09-01~03 scoped plan / GitHub WIF / durable journal | `4c972d98` | `e61beed4` | `a8206468` | 30 | 로컬+상위 스택 경유 |
| #48 | K09-04A Cloudflare inactive version adapter | `e61beed4` | `1edf3cc5` | `0032a5ea` | 2 | #54 CI 경유 |
| #50 | K09-04B journaled observe/reconcile | `1edf3cc5` | `378b59e6` | `302d58bf` | 7 | #54 CI 경유 |
| #55 | Worker Version tag 이중 검증 | `378b59e6` | `87bc3a34` | `e6ac3752` | 2 | #57 CI 경유 |
| #59 | Free-only 서명 게이트 | `87bc3a34` | `e6c9d0fb` | `ae9bbf7b` | 3 | #61 CI (직접) |
| #49 | K10-01 정적 키 폐기 계획 | `e61beed4` | `ffc83ea0` | `2d763a04` | 2 | #53/#54 경유 |
| #51 | K10-02 준비된 세대 | `ffc83ea0` | `8dce0c6d` | `72a10523` | 2 | #53/#54 경유 |
| #52 | K10-03 소비자 증명 | `8dce0c6d` | `82269ad4` | `83561007` | 2 | #53/#54 경유 |
| #53 | K10-04 1세대 폐기 시도 | `82269ad4` | `7e6da52f` | `c3d89f34` | 3 | #53/#54 경유 |
| #56 | provider 결속 old identity | `7e6da52f` | `54b087e3` | `a61491e3` | 6 | #57/#61 경유 |
| #58 | K10-05 read-only identity GET | `54b087e3` | `d9255689` | `8369a38e` | 6 | #61 CI (직접) |
| #62 | Journal 경로 계약 테스트 수정 | `d9255689` | `dd77ef65` | `adcc31e8` | 1 | #61 CI 포함 |
| #60 | K10-06 issuer/revoker 분리 | `d9255689` | `010d4d37` | `247484b3` | 6 | #61 CI (직접) |
| #63 | Cloudflare 토큰 만료 상한 | `010d4d37` | `18ff271c` | `45aa6b01` | 3 | #61 CI 포함 |

14개 PR의 고유 변경 경로는 총 **75개**(중복 제거 전 합계)다. 파일별 blob SHA는
[exact-source-manifest.json](exact-source-manifest.json)의 `owning_prs[].files`가 소유한다.

## 반드시 포함돼야 하는 5개 수정 — 실제 포함 확인

| 요구 | 소유 PR | 확인 방법 | 결과 |
|---|---|---|---|
| Worker Versions REST 응답 형태 수정 | #50 (`internal/target/cloudflare/version_observe.go`) | 후보 blob == `#50` head blob (`git diff-tree` 결과 `M` 없음) | 포함 |
| Version Tag 이중 검증 보강 | #55 (`version_observe.go`, `version_observe_test.go`) | 후보 blob == `#55` head blob, `TestVersionTagNeedsIndependentWorkerVersionAPIProof` 존재 | 포함 |
| 기존 자격증명 신원 보호 | #56 (`revocation_identity.go`, `finalize.go`, 가이드) | 후보 blob == `#56` head blob | 포함 (#61 Tree에서는 **문서 1건 누락**) |
| Journal 테스트 계약 수정 | #62 (`provideridentity_test.go`) | 후보 blob == `#62` head blob (`9bf1a525`) | 포함 |
| 토큰 만료 제한 | #63 (`providerlifecycle/cloudflare.go`) | 후보 blob == `#63` head blob, 5분 하한·24시간 상한 검사 | 포함 |

## 스택별 상세

### #47 — K09-01~03 (기반)

30개 경로: `internal/delivery/*`, `internal/delivery/journal/*`, `internal/identity/githubwif/*`,
`internal/target/gh/rest*.go`, `internal/cli/{auth,delivery,delivery_status}.go`, `go.mod`,
`docs/contracts/cli.md`, `docs/guides/delivery-plan.md`.
`go.mod` 변경은 `golang.org/x/sys` 승격 수준이며 `go.sum`은 변경 대상이 아니다(모듈 재해석 없음).

### #48 → #50 → #55 → #59 — Cloudflare/K09 계열

- #48: `version_stage.go` + 테스트만 추가. provider mutation 없음(합성 transport).
- #50: `version_stage.go`를 수정하고 `version_observe.go`, `cloudflaredelivery/{run,reconcile}.go`,
  `docs/guides/cloudflare-inactive-stage.md` 추가. reconcile는 **원본 journal write intent**에
  결속된다(`TestReconcileRequiresDistinctCurrentReadGrantAndExactOriginalWrite`).
- #55: `version_observe.go`의 tag 판정을 Wrangler 출력이 아니라 **독립 Worker Version API**로
  검증하도록 수정. #50이 만든 파일을 덮어쓰는 유일한 지점이므로 순서를 고정해야 한다.
- #59: `internal/delivery/freegate/gate.go` + `docs/guides/cloudflare-freegate-signed-pilot.md`.
  ed25519 Owner 서명, 계정 ID 고정, 실 재고(inventory) 일치, Free 플랜 근거를 요구하고
  불일치 시 provider read 없이 차단한다.

### #49 → #51 → #52 → #53 → #56 — K10 계열

- #49 `rotation/plan.go`, #51 `rotation/prepare.go`, #52 `rotation/attest.go`,
  #53 `rotation/finalize.go` + `docs/guides/credential-rotation-lifecycle.md`.
- #56이 `finalize.go`를 수정하고 `revocation_identity.go`를 추가한다. 즉 #53이 만든 파일을
  #56이 덮어쓰므로 #53 → #56 순서가 강제된다.
- #56은 `RevocationGrant.OldIdentityDigest` + 별도 `VerifyOldCredential` 콜백을 요구한다.
  "임의 JSON 해시"나 콜백 등록만으로는 승인되지 않는다.

### #58 → #62 → #60 → #63 — read-only 조회와 lifecycle 효과

- #58 `rotation/provideridentity/*` (GitHub/Cloudflare/OCI read-only GET) + 가이드.
- #62는 #58의 테스트가 임시 경로를 쓰던 것을 **실제 KeyCLI Journal 경로 계약**
  (절대·비심볼릭·0700)으로 고친다. 코드 변경 없음.
- #60 `rotation/providerlifecycle/*` — issuer(Create)와 revoker(Delete)를 분리하고
  host-scoped `HostAuthorize` 콜백을 요구한다.
- #63은 `CreateCloudflareAccountToken`에 `now+5m ~ now+24h` 만료 창을 강제한다.
  범위를 벗어나면 provider 호출 전에 거부한다.

## 누락·중복·충돌

- **중복 경로**: `docs/guides/credential-rotation-lifecycle.md`(#53 추가 → #56 수정),
  `internal/target/cloudflare/version_observe.go`(#50 추가 → #55 수정),
  `internal/rotation/finalize.go`(#53 추가 → #56 수정),
  `internal/rotation/provideridentity/provideridentity_test.go`(#58 추가 → #62 수정),
  `internal/rotation/providerlifecycle/{cloudflare.go,client_test.go}`와
  `docs/guides/provider-lifecycle-effects-v010.md`(#60 추가 → #63 수정).
  모두 **선형 후속 수정**이며 서로 다른 PR이 같은 blob을 두고 경쟁하지 않는다.
- **소스 충돌**: 0건. `git apply --check`가 14개 patch 전부 통과했고, blob overlay 방식도
  같은 Tree를 만들었다.
- **원본 PR에서 빠진 수정·문서·테스트**: 없다. 후보는 14개 PR head blob의 합집합과
  정확히 일치한다(교차 검증 결과는 [git-blob-parity-report.md](git-blob-parity-report.md)).
- **검증 전용 PR의 결함**: #61 Tree(`ac5c3d4a`)는 `#56`이 추가한 K10-04R 절을 담고 있지 않다.
  해당 경로 blob이 `#53` 시점(`13993e9e`)으로 되돌아가 있다. 후보는 `#56` head blob
  (`449296d2`)을 유지한다.

## 범위 밖 (섞지 않음)

- #46 설계 문서(=`docs/keycli-v09-multicloud-implementation-20261110`), #45 CI Guard,
  #29 Recipient, #42 Policy, #40 정책 마감은 독립 작업이다. 후보에 포함하지 않았다.
- #40은 현재 공유 checkout의 활성 브랜치(`docs/policy-lifecycle-closeout`)이므로
  해당 worktree는 건드리지 않았다.
