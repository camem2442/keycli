# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**2026-10-24 — 구현 전 설계 문서화.** 설계·선행 조사·결정을 문서로 고정. 코드 없음.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| 요구·설계 문서 | **COMPLETE** | 이 저장소 docs/ |
| 전송 설계 | **COMPLETE** (문서) | [contracts/transfer.md](docs/contracts/transfer.md) |
| 선행 조사 | **COMPLETE** | [reviews/2026-10-24-prior-art.md](docs/reviews/2026-10-24-prior-art.md) |
| 구현 | **DEFERRED** | owner 지시 전 시작 금지 |
| workspace-infra 등록 | **DEFERRED** | `registry/repositories.json` 미등록 |
| git 저장소 | **PARTIAL** | `git init`만, 커밋 없음 |

## 다음

1. owner가 설계 확인 → [roadmap](docs/planning/roadmap.md) M1 착수.
2. 열린 결정: [decisions](docs/decisions/) 의 "열린 질문" 절.

## NEXT CONTEXT

같은 채팅에서 설계를 확정한 상태. 구현은 owner가 명시적으로 지시할 때만.
