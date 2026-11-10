---
status: proposal
owner: keycli-delivery
last_verified_commit: 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace
---

# v0.9.x → v0.10.0 릴리스 계획 (제안)

현재 기준선: `main 4c972d98`, 최신 릴리스 **v0.8.0**(2026-11-08), 설치 pin도 v0.8.0
(`~/.local/bin/kc` → `kc version v0.8.0 (ad30607)`). 이 문서는 제안이며 어떤 태그도
아직 만들지 않았다.

## 사실 먼저: 후보가 실제로 노출하는 것

`internal/rotation`(K10 전체)은 **CLI에 연결돼 있지 않다**. 후보 Tree에서
`internal/rotation`을 import하는 패키지는 자기 자신뿐이고 `root.go`의 명령 등록에도 없다.
따라서 후보를 그대로 출시하면 사용자에게 보이는 변화는 K09 표면뿐이다.

| 표면 | 상태 | 비고 |
|---|---|---|
| `kc delivery plan --profile <name>` | 노출 | 오프라인 프로필 검증, `--json` metadata v1 |
| `kc delivery status` | 노출 | 로컬 journal만 읽음, provider 미접촉 |
| `kc auth probe --offline --profile --claims` | 노출 | 합성 claim fixture 비교, JWT 미수용·provider 미인증 |
| `internal/delivery/{githubdelivery,cloudflaredelivery,freegate}` | 라이브러리만 | CLI 미연결 |
| `internal/rotation/*` (K10-01~06) | 라이브러리만 | CLI 미연결 |

## 버전 배치 제안

| 버전 | 범위 | 진입 조건 |
|---|---|---|
| **v0.9.0** | K09 표면 3개 명령 + K09/K10 라이브러리 소스 | landing 병합 + CI green + 릴리스 산출물 + 릴리스 노트 |
| **v0.9.1** | 후속 수정(문서 회귀, macOS TMPDIR 등) | 필요 시 |
| **v0.10.0** | rotation CLI 노출 + provider 실계정 인수 | CLI 배선 Goal + 실 provider 승인 + mac-smoke |

이유: v0.9.0에 rotation을 넣으면 "명령은 없는데 버전만 올라간" 상태가 된다.
K10은 CLI·실 인수가 끝날 때 v0.10.0으로 올리는 편이 릴리스 이력과 사용자 기대에 맞다.

## v0.9.0 게이트

| ID | 게이트 | 근거 | 상태 |
|---|---|---|---|
| R1 | 소스 landing 병합 승인 | 사용자 명시 승인 (이번 범위 밖) | **HOLD** |
| R2 | landing PR CI green | 기존 #61 증거는 코드 동등 범위 재사용, PR test merge Tree를 후보 Tree와 대조 | 재사용 가능 / PR 시 재확인 |
| R3 | 문서 suite | `verify.sh --suite docs` 로컬 ok | **PASS (local)** |
| R4 | 릴리스 산출물 | `scripts/lifecycle.sh release` → kc 4타깃 + keycli-linux-amd64 + SHA256SUMS | 산출물 생성 PASS(local) / MCP 실행 단계는 Linux CI 전용 **NOT_VERIFIED** |
| R5 | mac-smoke | 실 Mac·실 keychain 필요 | **NOT VERIFIED (미실행)** |
| R6 | 설치 pin 갱신 | `~/.local/bin/kc` 교체 | **HOLD** |
| R7 | 릴리스 노트 | CLI 계약 v1 필드 추가 여부 확인(추가=호환) | 작성 필요 |

주의: macOS에서는 `verify-mcp-binary.sh`가 Linux 바이너리를 실행할 수 없어(exit 126) release suite 전체는 Linux CI에서만 성립한다. R4는 로컬 산출물 생성까지만 확인한다. 태그·GitHub Release 생성은
`RELEASE=not_authorized`이므로 수행하지 않는다.

## v0.10.0 선행 작업 (별도 Goal)

1. rotation CLI 배선(`kc rotation plan|prepare|attest|finalize` 등) + 계약 문서 갱신.
   계약 필드 **추가는 호환**(`v` 유지), 삭제·의미 변경만 `v` 상향.
2. `TRUSTED_HOST` 확보: WI-KC-02 (Trusted Host Admission + Writer/Grant validation).
3. 실 provider 인수(승인된 계정·범위·예산):
   - Cloudflare: Free 플랜 서명 증거 + inactive version stage/observe
   - GitHub: generation-scoped Actions Secret 1회 왕복
   - OCI: Vault Secret Version read-only identity 확인 → 삭제 예약은 최후
4. mac-smoke: 실 Mac, throwaway `kc.test.*`만.

## 릴리스 노트 골자 (v0.9.0)

- 추가: `kc delivery plan`, `kc delivery status`, `kc auth probe`(offline).
- 추가(라이브러리): Cloudflare inactive version stage/observe/reconcile, Free-only 서명 게이트,
  K10 rotation 계획·준비·증명·폐기·provider identity GET·lifecycle effect.
- 보안: provider mutation은 신뢰 호스트 콜백 없이는 실행되지 않음. 불확실 응답은
  `UNKNOWN_EFFECT`로 보존하고 자동 재시도하지 않음.
- 미검증: 실 provider 계정 동작, mac GUI 경로, rotation CLI(미배선).

## 롤백

landing을 단일 revert로 되돌릴 수 있도록 **선형 replay 브랜치**(`80bdd82b`)를 payload로
쓰는 것을 권장한다([merge-strategy-decision.md](merge-strategy-decision.md)).
revert 시 `dist/`·설치 pin·릴리스 태그는 별도로 되돌린다(태그는 생성 전이므로 해당 없음).
