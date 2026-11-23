---
status: source-only-pilot
date: 2026-11-23
extends: portable-native-oauth-discovery-r0.md
---

# Portable native OAuth discovery fetch R0

Package: `internal/auth/oauth/discoveryfetch`

`portable-native-oauth-discovery-r0`의 파서는 **일부러 네트워크가 없다**(`oauth` 패키지 doc:
"performs no network, browser, filesystem, or secret-store operations"). 이 패키지는 그 반대편
한 단계만 담당한다: well-known URL을 **한 번** GET해서 본문을 `oauth.ParseDiscovery`에 넘긴다.

패키지를 나눈 이유는 순수 경계를 import 그래프에 드러내기 위해서다. `oauth`를 import하는
코드는 여전히 네트워크가 없다는 것을 타입 수준에서 알 수 있다.

## 흐름

```text
oauth.DiscoveryURL(issuer)        // 발급자 핀 → well-known URL (HTTPS 강제)
GET, Accept: application/json     // 자격증명 없음
requireSameURL(target, resp.Request)
status == 200
Content-Type == application/json 또는 application/*+json
io.LimitReader(body, MaxBytes+1)
oauth.ParseDiscovery(body, issuer) // issuer exact match 등 재검증
```

## 거부 규칙

| 조건 | 오류 |
| --- | --- |
| `doer`가 nil, 요청 실패, 응답/본문 nil, 읽기 실패, ctx 취소 | `ErrTransport` |
| 응답의 `Request.URL`이 요청한 well-known URL과 다름, 또는 `Request` 없음 | `ErrRedirect` |
| status ≠ 200 | `ErrStatus` |
| Content-Type이 JSON이 아님(예: 로그인 페이지의 `text/html`) | `ErrContentType` |
| 본문이 `MaxBytes` 초과(기본 `oauth.MaxDiscoveryBytes` = 64KB) | `ErrTooLarge` |
| 발급자 자체가 신뢰 불가 | `oauth.ErrDiscoveryIssuer` (요청 시도 전) |
| 문서가 다른 issuer를 설명 | `oauth.ErrDiscoveryIssuer` |
| 그 밖의 문서 결함 | `oauth.ErrDiscovery*` |

**리다이렉트는 사후에 거부한다.** `*http.Client`는 기본으로 리다이렉트를 따라가므로, 우리가
요청한 URL과 실제 응답을 만든 URL을 비교한다. 사후 비교인 이유는 `Doer` 인터페이스가
`CheckRedirect`를 노출하지 않기 때문이며, 자격증명을 보내지 않으므로 추종 자체의 피해는
"다른 발급자의 문서를 받아들일 뻔한 것"으로 한정된다.

**본문 64KB 상한은 파서 상한과 같다.** 여기서 먼저 자르면 파서가 거대한 입력을 읽지 않는다.

## 주장하지 않는 것

1. **캐시 없음** — 호출마다 네트워크. 만료·ETag·If-None-Match는 미구현.
2. **재시도·백오프 없음** — 한 번 실패하면 그대로 반환. 재시도는 정책 결정이며 호출자 몫.
3. **TLS 검증은 `Doer` 책임** — `*http.Client`는 기본 검증을 한다. 커스텀 `Doer`를 넣는
   호출자는 그 사실을 알고 있어야 하며, 이 패키지는 검증을 강제할 수 없다.
4. **`Doer` 신뢰** — `Doer`가 `Request.URL`을 거짓으로 보고하면 핀 검사는 속는다. 이는
   같은 프로세스의 코드이므로 위협 모델 밖이다.
5. **DCR·JWKS·토큰 교환** — 미구현.
6. **프록시·mTLS·관측** — 호출자가 `Doer`로 주입.

## 검증

```text
gofmt -l internal/auth/oauth/discoveryfetch/   출력 없음
go vet ./internal/auth/oauth/discoveryfetch/...  PASS
go test -count=1 ./internal/auth/oauth/discoveryfetch/...  PASS
go test -race -count=1 ...                                  PASS
go build ./...                                              PASS
```

테스트는 네트워크를 쓰지 않는다(`Doer` fake). 테스트가 실제로 방어선을 고정하는지는
변이로 확인했다.

| 변이 | 결과 |
| --- | --- |
| 리다이렉트 URL 비교 제거 | `TestFetchRefusesRedirectedResponse`, `...WithoutProvenance` **FAIL** |
| Content-Type 검사 제거 | `TestFetchRefusesStatusAndContentType` **FAIL** |
| 본문 상한 제거 | `TestFetchBoundsTheBody` **FAIL** |
| `Accept` 헤더 제거 | `TestFetchReturnsParsedMetadata` **FAIL** |

실 제공자 1회 확인(임시 테스트, 커밋 안 함):

```text
Fetch(ctx, &http.Client{}, "https://white-brook-b162.cloudflareaccess.com", 15s)
→ issuer/ token/ revoke 3개 엔드포인트 수용 (0.68s)
```

## 남는 미검증

- Linux/Flip3 exact-head 게이트 — **NOT VERIFIED**
- 실제 리다이렉트·프록시·TLS 실패 경로의 실행 검증 — **NOT VERIFIED** (fake `Doer`로만 확인)
- 캐시·재시도는 설계상 없음(위 "주장하지 않는 것")
