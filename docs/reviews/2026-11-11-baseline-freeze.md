---
status: historical
owner: delivery
last_verified_commit: bf7a8b3ea50844b5312d1abfddf10ad982a16af7
---

# S0 — Baseline Freeze 수령서 (2026-11-11)

## 고정 기준

| 저장소 | 브랜치 | HEAD | Tree |
|---|---|---|---|
| keycli (제품 소스) | `landing/keycli-v010-source-history-20261110` | `18d4cf1` | `de942184` |
| keycli (host bridge/OCI) | `opencode/kc-hostbridge-20261110` | **`bf7a8b3`** | `7caa1bcb` |
| workspace-infra | `opencode/keycli-delivery-receipt-20261110` | **`abe6f62`** | `709f35a1` |
| `origin/main` (keycli) | — | `4c972d98` | `6568e233` |
| landing payload | — | `80bdd82b` | `ee735340` |

선행 기준 `7f19eb8`은 `bf7a8b3`의 조상임을 확인했다(`git merge-base --is-ancestor`).

## Worktree / WIP

| 경로 | 상태 | 조치 |
|---|---|---|
| keycli 공유 checkout | `0e45e9c` (`docs/policy-lifecycle-closeout`), dirty 0 | **건드리지 않음** |
| workspace-infra 공유 checkout | `99657d1`, 기존 WIP 11건 | **건드리지 않음** |
| keycli `v010-landing` / `v010-hostbridge` | clean | 작업용 |
| workspace-infra `keycli-delivery-receipt-20261110` | clean | 작업용 |

## 기존 구현 지도

| 계층 | 위치 | 역할 |
|---|---|---|
| Claim / generation / lease | `workspace-infra/tooling/goal_execution_store.py` (`SqliteClaimStore`, `admit_mutation`, `fenced`) | claim fencing (원자 takeover, STALE_GENERATION, SESSION_MISMATCH, DUPLICATE, lease) |
| Grant / budget | `tooling/goal_authority_core.py` (`resolve_authority`, `consume_budget`) | authorize·예산 (nullable budget 수정 포함) |
| Host admission | `tooling/keycli_host_admission.py` | effect 직전 1회 판정 (HOST-01~30) |
| Receipt projection | `tooling/keycli_delivery_receipt.py` | read-only, 비권위 |
| Policy receipt | `tooling/keycli_policy_receipt.py` | P5 policy projection (기존, 미수정) |
| Journal | `keycli/internal/delivery/journal` | 로컬 flock, durable intent, `UNKNOWN_EFFECT` 래치 |
| Host bridge | `keycli/internal/hostbridge` | callback 게이팅 (in-process) |
| OCI WIF | `keycli/internal/identity/ociwif` | source-only |
| Isolation canary | `keycli/scripts/isolation-canary.sh` | dry-run 기본, `--execute`는 peer UID 필요 |

**Effect fencing 구현체는 없다** → [effect-fencing-decision-v1.md](../../../workspace-infra/docs/contracts/effect-fencing-decision-v1.md).

## 회귀 결과 (이 HEAD에서 실측)

| 검사 | 명령 | 결과 |
|---|---|---|
| keycli 전체 Go | `go test -tags=kc_testhooks ./...` (Go 1.22.12) | **39/39 패키지 ok, 실패 0** |
| keycli docs / policy | `verify.sh --suite docs` / `policy` | ok / ok |
| infra KeyCLI suites | `verify.py --suite keycli-delivery-receipt --suite keycli-host-admission` | **82 PASS** |
| infra authority | `verify.py --suite authority` (CPython 3.12) | **122 PASS** |
| 합계 (infra) | 위 3 suite | **204 PASS, 실패 0** |

`python3` 기본값(3.10.8)이 아니라 문서화된 **CPython 3.12**를 사용했다. 3.10에서는
`-P` 미지원으로 `test_goal_managed_delivery`가 실패하는데, 이는 소스 문제가 아니다.

## 남은 HOLD (이 작업 소유 아님)

| 항목 | 상태 | 소유 |
|---|---|---|
| `planning-docs`: `clv-44-phase3-parallel-plan-2026-11-04.md` 링크 | 파일이 어떤 ref에도 없음 → HOLD | CLV planning writer |

## Gate

```
BASELINE_PASS = PASS
S1 (cross-UID) = BLOCKED (peer UID 실행 권한 없음)
```

S0 증거는 이 문서와 [2026-11-11-evidence-closure.md](2026-11-11-evidence-closure.md),
[2026-11-11-cross-uid-alternatives.md](2026-11-11-cross-uid-alternatives.md)가 소유한다.
