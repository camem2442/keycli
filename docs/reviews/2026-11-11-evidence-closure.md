---
status: historical
owner: delivery
---

# 독립 증거 인수·폐쇄 (2026-11-11, H0–H5)

기존 WI-KC/호스트 브리지/OCI 구현을 **재작성하지 않고** 독립 검증했다.
기준: keycli `7f19eb8` (tree `6c456bb6`), workspace-infra `b99824a` (tree `da5cbf33`).

## H0 — 증거 재검사

| 대상 | 결과 |
|---|---|
| Python (WI-KC-01 22 + WI-KC-02 48 + budget 12) | **82 PASS** (`verify.py --suite keycli-*`) |
| Go hostbridge | **20 PASS** |
| Go OCI WIF | **16 PASS** |
| H20/H21/H22 실제 journal | 동일 결과 재현 (1/8 성공, 재시작 후 redispatch 0, 응답 유실 재시도 0) |
| nullable budget | baseline 재현 → 12 케이스 PASS |
| 두 receipt 모듈 독립성 | 상호 import 0건 (정책 projection vs delivery projection) |

소스가 바뀌지 않은 부분은 전체 suite를 반복하지 않았고, 변경된 모듈만 재실행했다.

## H1 — nullable budget 의미 보존

12개 케이스가 모두 통과했고, **null 예산은 생략과 완전히 동일하게 동작**한다
(`remaining_budget=None`, `budget_effect=RESERVE`). scope·effect·stage·expiry는
그대로 차단되므로 승인 범위가 넓어지지 않는다.

## H2 — baseline red 분류

| 실패 | 분류 | 근거 | 조치 |
|---|---|---|---|
| `test_goal_managed_delivery` 6건 | **환경(인터프리터)** | 테스트가 `python -P`(3.11+)를 spawn하는데 로컬 기본은 pyenv **3.10.8** → `Unknown option: -P`, exit 2. 문서화된 런타임 **CPython 3.12**에서 **36/36 PASS**, `authority`+`goal-preflight` **138 PASS** | 코드 수정 없음, 런타임만 교체 |
| `planning-docs` 1건 | **기존 깨진 링크** | `clv-35-…closeout`이 `clv-44-phase3-parallel-plan-2026-11-04.md`를 링크하나 이 파일은 **어떤 ref에도 존재한 적 없음** | CLV planning writer 소유 → **HOLD** (수정하지 않음) |
| `internal/cursor` 1건 | **환경 + 테스트 결함** | 실제 Cursor 실행 중 가드 발동. 호스트 상태에 의존하는 테스트 | **최소 수정**: `observeCursor` seam 추가(프로덕션 동작 불변), 테스트가 관측을 주입 → 결정적 통과 |

교훈: "baseline red"라고 뭉뚱그리지 않고 (a) 인터프리터 (b) 깨진 링크
(c) 호스트 상태 의존 테스트로 분리했다. 어느 것도 필수 릴리스 게이트를 자동
통과시키지 않는다.

## H3 — 크래시·중복

기존 실측을 그대로 인수한다. 추가 테스트는 필요하지 않았다.
H23은 **로컬 journal root 일치 검사**까지만 증명하며, cross-host fencing으로
승격하지 않는다.

## H4 — 권한 경계·격리

| Gate | 판정 | 근거 |
|---|---|---|
| `SAME_UID_APPLICATION_GUARD` | **PASS (application)** | HOST-11/12/16/28/30: forged decision은 effect 전에 거부, same-uid mutation 차단, 토큰 redaction |
| `CROSS_UID_SECRET_ISOLATION` | **NOT_VERIFIED** | `sudo -n` 불가(password required) → peer UID 실행 불가. 계정 생성은 권한 밖 |
| `CROSS_HOST_WRITER` | **PARTIAL** | root digest 고정 + 두 root 독립(H23)까지만 |
| `TRUSTED_HOST` | **SOURCE_PARTIAL** | 실제 호스트 프로세스 격리 미증명 |

준비물: `scripts/isolation-canary.sh` (기본 dry-run, `--execute`는 passwordless sudo 필요,
synthetic canary만 사용, 실제 Keychain·provider token 미사용). 현재 실행 결과:

```
status=NOT_VERIFIED reason=no-passwordless-sudo-for-peer
```

## H5 — 문서

| 문서 | 갱신 |
|---|---|
| `docs/contracts/hostbridge-v1.md` (keycli) | 격리 매트릭스 참조, cursor seam은 아래 참조 |
| `docs/contracts/host-isolation-acceptance-v1.md` (infra) | 3계층 증거 매트릭스·폐쇄 절차 |
| `docs/contracts/keycli-host-admission-v1.md` (infra) | cross-host 한계, nullable-budget 12케이스 |
| `docs/reviews/2026-11-10-wi-kc-02-host-admission.md` | H0–H4 폐쇄 기록 |

## 최종

```
PYTHON_TESTS              82 PASS (keycli suites) / 138 PASS (authority+preflight, py3.12)
GO_TESTS                  20 PASS
OCI_TESTS                 16 PASS
AUTHORITY_REGRESSION      PASS (12-case nullable budget, semantics unchanged)
BASELINE_RED              managed_delivery=env(py3.10), planning-docs=broken link(other writer, HOLD),
                          cursor=env(tests now deterministic via seam)
JOURNAL_CRASH_RECOVERY    PASS (H20/H21/H22 on the real journal)
SAME_UID_APPLICATION_GUARD PASS (application level)
CROSS_UID_SECRET_ISOLATION NOT_VERIFIED (no peer-UID privilege)
CROSS_HOST_WRITER         PARTIAL (local root pinning only)
TRUSTED_HOST              SOURCE_PARTIAL
```
