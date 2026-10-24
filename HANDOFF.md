# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**M6 완료:** resolver 미들웨어 체인, `audit`·`policy`, `kc audit`, `kc policy check`, `ls` 정책 표시.

## 상태

| 영역 | 상태 | 근거 |
|---|---|---|
| CLI M1–M5 | **COMPLETE** | v0.4.0 |
| M6 audit + policy middleware | **COMPLETE** | Linux `./verify.sh --suite all` |
| `kc audit` / `kc policy check` | **COMPLETE** | 단위·integration |
| [0005-m6-audit-policy-middleware.md](docs/decisions/0005-m6-audit-policy-middleware.md) | **COMPLETE** | |
| mac-smoke (Darwin) | **NOT VERIFIED** | Linux CI skip |
| 샌드박스 종료 3 (실 Mac) | **NOT VERIFIED** | |
| Cursor Cloud 실 push | **NOT VERIFIED** | |
| 에이전트 실사용 + 정책 운영 | **NOT VERIFIED** | |
| TTL 캐시 / rate limit / masking / 1Password | **DEFERRED** | roadmap |

## 검증

```bash
./verify.sh --suite all
./verify.sh --suite mac-smoke   # Darwin
```

## NEXT CONTEXT

실 Mac mac-smoke. 에이전트별 `middleware`·`policy.agent.*` 운영 1회. 외부 `kc-target-*`(DEFERRED).
