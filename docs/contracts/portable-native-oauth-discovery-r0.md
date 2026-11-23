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
퍼센트 인코딩된 경로는 **한 번만** 인코딩되고, 인코딩된 구분자도 보존된다.

- `/a%20b` → `…/a%20b` (`%2520`이 되지 않는다)
- `/a%2Fb` → `…/a%2Fb` (한 세그먼트. 디코드된 `Path`만 쓰면 `/a/b`로 갈라진다)

구현은 `Path`(디코드)와 `RawPath`(이스케이프)를 **둘 다** 채워 `URL.String()`이 한 번만
인코딩하면서 구분자 정보를 잃지 않게 한다.

**엔드포인트 길이는 2048자로 제한**한다(`Begin`이 같은 URL에 적용하는 상한과 동일).

## ParseDiscovery — 거부 규칙

| 조건 | 오류 |
| --- | --- |
| `issuer`가 핀 값과 **문자 단위로 다름** (trailing slash, 대소문자, host, scheme 포함) | `ErrDiscoveryIssuer` |
| 핀 issuer 자체가 https가 아니거나 userinfo·query·fragment 포함 | `ErrDiscoveryIssuer` |
| `authorization_endpoint`/`token_endpoint`가 있으나 https 아님·userinfo·query·fragment·port 0·2048자 초과 | `ErrDiscoveryEndpoint` |
| 두 엔드포인트가 동일 URL | `ErrDiscoveryEndpoint` (**정책**, RFC 8414 요구 아님) |
| `response_types_supported`에 `code` 없음(필드 생략 포함) | `ErrDiscoveryEndpoint` |
| `code_challenge_methods_supported`에 `S256` 없음(생략 포함) | `ErrDiscoveryPKCE` |
| `token_endpoint_auth_methods_supported`가 있고 `none`이 없음 | `ErrDiscoveryClientAuth` |
| `revocation_endpoint`가 있는데 https가 아님 | `ErrDiscoveryEndpoint` |
| 필수 멤버(`issuer`·`authorization_endpoint`·`token_endpoint`) 누락, 타입 불일치 | `ErrDiscoveryDocument` |
| 중복 키, JSON 아님, **잘린 JSON**, 후행 콘텐츠, 64KB 초과, scope token 형식 위반 | `ErrDiscoveryDocument` |

**멤버 이름은 정확히 일치해야 한다.** `encoding/json`의 구조체 매칭은 대소문자와 일부
유니코드 폴드를 무시하므로, `token_endpoint`와 `Token_Endpoint`를 함께 넣으면 나중 값이
이긴다. 그래서 이 파서는 최상위를 `map[string]json.RawMessage`로 디코드하고 **정확한
이름의 멤버만** 읽는다. 잘못된 표기의 멤버는 "없는 멤버"로 취급된다.

**issuer 정규화는 하지 않는다.** RFC 8414 §3.3이 요구하는 것은 exact match이고,
`https://issuer`와 `https://issuer/`를 같은 것으로 취급하면 그 순간 impersonation
여지가 생긴다.

**PKCE는 필수다.** `code_challenge_methods_supported`가 비어 있으면 "광고 안 함"이며,
공용 클라이언트인 이 코어에는 쓸 수 없으므로 거부한다.

**중복 키는 직접 거부한다.** `encoding/json`은 마지막 값을 조용히 채택하므로, 같은 문서에
정상 `issuer`와 악성 `token_endpoint`를 함께 넣는 형태를 막기 위해 토큰 단위로 순회한다.

**확장 멤버는 무시한다.** RFC 8414는 확장 레지스트리이며, 미래 멤버를 거부하면 제공자
업데이트에 취약해진다. 단 문서는 **어디에서든 중복 키가 없어야** 한다(확장 객체 내부 포함).
값은 무시하지만, 같은 이름이 두 번 나오는 문서는 해석이 하나로 정해지지 않으므로 fail-closed로
거부한다.

**두 엔드포인트 동일 URL 거부와 S256/`none` 요구는 정책이다.** RFC 8414가 요구하는 항목이
아니며, 이 클라이언트가 실제로 쓰는 흐름(공용 클라이언트 + authorization code + PKCE)에
필요해서 넣었다. 다른 제공자를 붙일 때는 이 세 줄을 먼저 검토해야 한다.

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
