---
status: historical
owner: delivery
last_verified_commit: ed68ccd
---

# S5-D — v0.10.0 개발 채널 인수 증거 (2026-11-11)

기준: keycli `ed68ccd` (tree `4145ecfd`), workspace-infra `8d3a48e`.
이 문서는 `./dev release-check --channel dev`가 읽는 증거다.

## 게이트와 실행 결과

| Gate | 실행 | 결과 |
|---|---|---|
| `SOURCE_REGRESSION` | `go test ./...` (Go 1.22.12) | **40 패키지 ok, 306 테스트 PASS, 실패 0** |
| `PROTOCOL_FENCING` | S3/S4 2-host 시나리오 | **12 PASS** (60ms barrier, 단일 effect) |
| `UNKNOWN_EFFECT_RECOVERY` | S4-D 손실 응답 3단계 | **3 PASS** (UNKNOWN_EFFECT → 재시작 DUPLICATE → reconcile OBSERVED_APPLIED) |
| `DEV_PROVIDER_BUILD_LOCK` | `go test -run 'Lock\|Operational\|Runtime\|Report'` | **6 PASS** |
| `DEV_PROVIDER_RUNTIME_LOCK` | 같은 suite의 runtime/env 케이스 | **4 PASS** (env 단독·비정확 값·위조 요청·locked provider) |

## Provider 이중 잠금

```
build:   -X .../effectgateway.channel=operational   (기본값 dev)
runtime: KC_PROVIDER_EFFECTS=enabled                (기본값 미설정)
```

- 둘 다 충족해야만 `AllowRealProvider`가 통과한다.
- dev 바이너리 리포트: `dev-lock-report.json` → `provider_enabled=false`.
- `-provider live`는 잠금 상태에서 기동을 거부한다.
- 잠금 판정은 **요청 데이터(Grant/Claim/Envelope)를 입력으로 받지 않는다** →
  위조 Grant/Claim으로 해제 불가.

## 정직한 한계

- 이 게이트는 **개발 채널**이다. `PROVIDER_LIVE=NOT_VERIFIED`,
  `PRODUCT_CREDENTIAL_ISOLATION=NOT_VERIFIED`가 그대로 남는다.
- `release-check`는 증거 무결성(sha256 + 커밋 결속 + 실행 횟수)을 검사한다.
  서명이 없으므로 **위조 불가능을 증명하지는 않는다** — 저장소 쓰기 권한자가
  증거와 manifest를 함께 바꾸면 통과한다. branch protection을 대체하지 않는다.
- 실제 provider adapter는 아직 존재하지 않는다. 잠금은 "존재하지 않음"이 아니라
  "있더라도 실행 경로에 들어올 수 없음"을 구조적으로 보장한다.

## 연결 문서

- 채널 계약: workspace-infra `docs/contracts/release-channels-v1.md`
- 격리 매트릭스: workspace-infra `docs/contracts/host-isolation-acceptance-v1.md`
- S4 증거: `docs/reviews/2026-11-11-s4-gateway-host.md`
