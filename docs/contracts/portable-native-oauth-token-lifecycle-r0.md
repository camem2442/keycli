---
status: source-only-pilot
date: 2026-11-23
extends: portable-native-oauth-r0.md
---

# Portable native OAuth token lifecycle R0

Package: `internal/auth/oauth` (`lifecycle.go`)

Scope: **pure decisions about tokens the caller already holds.** No network, no keychain,
no filesystem, no clock, no revocation call, no token storage. The caller passes the
token set and the current time and gets an action it must then perform itself.

This closes the deliberate gap left by
[portable-native-oauth-r0](portable-native-oauth-r0.md), which states that "token
exchange or refresh" and Pending consumption belong to the caller. G4 still owns the
adapter, the protected store and the end-to-end proof; this file only removes the
decision ambiguity those pieces would otherwise each invent.

## Decisions

`Plan(tokens, now, policy) (Action, error)`

| 조건 | 결과 |
| --- | --- |
| access token이 `now + Skew` 이후까지 유효 | `UseAccessToken` |
| 만료됐거나 `Skew` 안쪽 | refresh token 있으면 `Refresh`, 없으면 `Reauthorize` |
| access token 없음 + refresh token 있음 | `Refresh` |
| access token 없음 + refresh token 없음 | `Reauthorize` |
| `TokenType`이 비어있지 않은데 Bearer가 아님 | `Reauthorize` + `ErrTokenSet` |
| `ExpiresAt`이 zero | `Reauthorize` + `ErrTokenSet` (무기한으로 해석하지 않는다) |
| access token에 공백/개행 포함 | `Reauthorize` + `ErrTokenSet` |

`Skew`는 "만료 직전에 요청을 보내는" 실패를 막기 위한 것이다. 값은 호출자가 정한다.

`ApplyRefresh(previous, next, now, policy) (Tokens, Rotation, error)`

- refresh 응답에 **access token이 없으면 채택하지 않는다**.
- refresh token을 **생략**하면 "변경 없음"(RFC 6749 §6)으로 보고 이전 값을 유지한다.
- 돌려받은 refresh token이 이전과 **같으면 회전이 아니다**(`RotationNone`). 회전으로
  기록하면 재사용 탐지가 무의미해진다.
- **scope는 좁아질 수만 있다.** 새 scope token이 이전 grant에 없으면 거부한다.
- 채택 전에 `Plan`을 다시 통과시켜, 저장하면 즉시 사용 불가가 될 조합을 막는다.
- 거부된 갱신은 저장된 토큰 집합을 **바꾸지 않는다**.

`PlanLogout(tokens) Logout`

- refresh token이 있으면 `RevokeRefresh=true`, 항상 `ClearStored=true`.
- access token만 있는 경우엔 revoke를 요청하지 않는다(제공자별로 무시되고 수명이 짧다).

## 비밀값 취급

`Tokens`는 `String()`/`GoString()`이 값을 노출하지 않는다. 이 패키지는 토큰을 로그·영수증·
stdout으로 내보내는 경로를 만들지 않는다. 실제 저장은 여전히 호출자(향후 per-app 보호
백엔드)의 책임이며, 이 파일은 저장을 대신하지 않는다.

## 남은 G4 경계 (이 문서가 주장하지 않는 것)

1. RFC 8414 discovery 어댑터(issuer 핀 고정) — **미구현**
2. 실제 token endpoint 교환·refresh 호출 — **미구현**
3. per-app 암호화 저장(Mac Keychain / Linux age) — **미구현**
4. revoke 요청 실제 전송과 실패 처리 — **미구현**
5. Pending 1회 소비·로그인 시간창 강제 — 호출자 책임, 게이트 **NOT VERIFIED**

## 검증

```text
gofmt -l (이 두 파일) 결과 없음
go vet ./internal/auth/oauth/...            PASS
go test -count=1 ./internal/auth/oauth/...  PASS
go test -race -count=1 ...                  PASS
go build ./...                              PASS
```

로컬 macOS(arm64) 결과이며, Linux/Flip3 exact-head 게이트는 이 PR에서 실행하지 않았다.
