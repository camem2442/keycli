---
status: source-only-pilot
date: 2026-11-23
extends: portable-native-oauth-r0.md
---

# Portable native OAuth discovery parsing R0

Package: `internal/auth/oauth` (`discovery.go`)

Scope: **RFC 8414 metadata를 해석·검증하는 순수 함수.** 네트워크 요청·캐시·재시도·DCR은
포함하지 않는다. 호출자가 `DiscoveryURL`로 URL을 만들고 HTTPS로 가져온 뒤 본문을
`ParseDiscovery`에 넘긴다. 이 분리의 목적은 **"이 문서가 정말 내가 핀한 issuer에 대한
것인가"** 라는 신뢰 판단을 네트워크 없이 테스트하는 것이다.

`portable-native-oauth-r0`는 "provider endpoints는 신뢰된 호출자 제공 메타데이터이며
issuer-pinned RFC 8414 discovery 어댑터를 통해 얻어야 한다"고만 적고 구현을 남겨 두었다.
이 파일이 그 앞단을 채운다.

## DiscoveryURL — RFC 8414 §3.1

| issuer | 결과 |
| --- | --- |
| `https://h` | `https://h/.well-known/oauth-authorization-server` |
| `https://h/` | `https://h/.well-known/oauth-authorization-server` |
| `https://h/tenant` | `https://h/.well-known/oauth-authorization-server/tenant` |
| `https://h/tenant/` | `https://h/.well-known/oauth-authorization-server/tenant` |

경로가 있는 issuer는 **well-known 뒤에** 붙는다(앞에 붙이면 다른 규격의 규칙이다).

## ParseDiscovery — 거부 규칙

| 조건 | 오류 |
| --- | --- |
| `issuer`가 핀 값과 **문자 단위로 다름** (trailing slash, 대소문자, host, scheme 포함) | `ErrDiscoveryIssuer` |
| 핀 issuer 자체가 https가 아니거나 userinfo·query·fragment 포함 | `ErrDiscoveryIssuer` |
| `authorization_endpoint`/`token_endpoint` 없음·https 아님·userinfo·query·fragment·port 0 | `ErrDiscoveryEndpoint` |
| 두 엔드포인트가 동일 URL | `ErrDiscoveryEndpoint` |
| `response_types_supported`에 `code` 없음(필드 생략 포함) | `ErrDiscoveryEndpoint` |
| `code_challenge_methods_supported`에 `S256` 없음(생략 포함) | `ErrDiscoveryPKCE` |
| `token_endpoint_auth_methods_supported`가 있고 `none`이 없음 | `ErrDiscoveryClientAuth` |
| `revocation_endpoint`가 있는데 https가 아님 | `ErrDiscoveryEndpoint` |
| 중복 키, JSON 아님, 후행 콘텐츠, 64KB 초과, scope token 형식 위반 | `ErrDiscoveryDocument` |

**issuer 정규화는 하지 않는다.** RFC 8414 §3.3이 요구하는 것은 exact match이고,
`https://issuer`와 `https://issuer/`를 같은 것으로 취급하면 그 순간 impersonation
여지가 생긴다.

**PKCE는 필수다.** `code_challenge_methods_supported`가 비어 있으면 "광고 안 함"이며,
공용 클라이언트인 이 코어에는 쓸 수 없으므로 거부한다.

**중복 키는 직접 거부한다.** `encoding/json`은 마지막 값을 조용히 채택하므로, 같은 문서에
정상 `issuer`와 악성 `token_endpoint`를 함께 넣는 형태를 막기 위해 토큰 단위로 순회한다.

**확장 멤버는 무시한다.** RFC 8414는 확장 레지스트리이며, 미래 멤버를 거부하면 제공자
업데이트에 취약해진다.

## 남는 경계 (이 파일이 주장하지 않는 것)

1. 실제 HTTPS 요청·재시도·타임아웃 — **미구현**(호출자/어댑터)
2. 메타데이터 캐싱·만료 정책 — **미구현**
3. DCR(동적 클라이언트 등록) — **미구현**, AI Usage v2는 Cloudflare 콘솔에서 등록된 clientID를 쓴다
4. `jwks_uri`·키 회전 — **미구현**, Access JWT 검증은 origin(별도 서비스)의 몫이다
5. 실제 제공자 문서에 대한 실행 검증 — **NOT VERIFIED**(아래)

## 검증

```text
gofmt -l (변경 파일)                        출력 없음
go vet ./internal/auth/oauth/...            PASS
go test -count=1 ./internal/auth/oauth/...  PASS
go test -race -count=1 ...                  PASS
go build ./...                              PASS
```

로컬 macOS(arm64) 결과다. Linux/Flip3 exact-head 게이트와 **실제 Cloudflare Access
메타데이터를 가져와 파싱하는 실행**은 이 PR에서 하지 않았다(`NOT VERIFIED`).
