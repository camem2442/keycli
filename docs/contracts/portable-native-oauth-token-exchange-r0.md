---
status: source-only-pilot
date: 2026-11-23
extends: portable-native-oauth-r0.md
---

# Portable native OAuth token exchange R0

Packages:
- `internal/auth/oauth` — `BuildExchange`(요청 폼), `ParseTokenResponse`/`ParseTokenError`(응답 파서), **순수**
- `internal/auth/oauth/tokenexchange` — 폼 POST와 응답 수신, 네트워크

`portable-native-oauth-r0`의 `Begin`은 브라우저를 열고 콜백까지 처리하지만 "The caller owns
... token exchange"라고 남겨 두었다. 이 파일이 그 교환을 채운다. 파서·빌더는 여전히 시계·네트워크·
저장소가 없고, 호출자가 `now`를 넘긴다.

## 요청 — RFC 6749 §4.1.3

| 필드 | 값 | 이유 |
| --- | --- | --- |
| `grant_type` | `authorization_code` | 고정 |
| `code` | 콜백 코드 | 빈 값·공백·제어문자·4096자 초과 거부 |
| `redirect_uri` | `Pending.RedirectURI` | `client.RedirectURI`와 **일치**해야 함 |
| `client_id` | `Client.ID` | 공용 클라이언트라 secret 없음 |
| `code_verifier` | `Pending.CodeVerifier` | RFC 7636: 43–128자 unreserved |
| `resource` | `Pending.Resource` | 있을 때만, `Client.Resource`와 일치(RFC 8707) |

`Pending.Issuer`가 `Provider.Issuer`와 다르면 거부한다. `Begin`이 만든 상태와 다른 발급자에
코드를 보내는 것은 재사용·오배송의 시작이다.

**`scope`는 보내지 않는다.** 코드 교환에는 포함하지 않는 것이 RFC 6749의 형태이고, 일부
제공자는 이 필드가 있으면 요청을 거부한다.

## 응답 — RFC 6749 §5

| 조건 | 결과 |
| --- | --- |
| `access_token` 없음·빈 값·공백/비ASCII·8192자 초과 | `ErrTokenResponse` |
| `token_type`이 있고 `Bearer`가 아님(대소문자 무시) | `ErrTokenResponse` |
| `token_type` 생략/빈 값 | 허용(이 클라이언트는 bearer만 보냄) |
| `refresh_token`이 있고 공백/비ASCII·8192자 초과 | `ErrTokenResponse` |
| `expires_in`이 정수 아님(문자열·소수·지수), ≤0, 1년 초과 | `ErrTokenResponse` |
| `expires_in` 생략 | 허용, `ExpiresAt`은 zero(수명 미상) |
| `scope` 토큰이 RFC 6749 §3.3 형식 위반 | `ErrTokenResponse` |
| 중복 키, JSON 아님, 후행 콘텐츠, 64KB 초과 | `ErrTokenResponse` |
| `error` + `access_token` 동시 | `ErrTokenResponse` (어느 쪽도 신뢰 불가) |

### 오류 분류

| 코드 | 오류 | 의미 |
| --- | --- | --- |
| `invalid_request` `invalid_client` `invalid_grant` `unauthorized_client` `unsupported_grant_type` `invalid_scope` `access_denied` | `ErrTokenRejected` | 같은 요청 재시도 무의미 → 재인증 |
| `server_error` `temporarily_unavailable` | `ErrTokenTransient` | 나중에 다시 시도 가능(자동 재시도 없음) |
| 그 밖 | `ErrTokenResponse` | 분류 불가 |

`error_description`은 **오류 메시지에 넣지 않는다.** 서버가 보낸 텍스트를 로그·영수증으로
흘리는 경로를 만들지 않기 위해서다.

`error` 코드도 그대로 넣지 않는다. 64자 이하이면서 scope-token 형식(printable ASCII, `"`·`\`
제외)일 때만 `%q`로 인용해 포함하고, 그 밖은 `ErrTokenResponse`로 끝낸다. CR/LF나 임의의 긴
문자열이 로그 한 줄에 들어가는 것을 막기 위해서다.

`scope`는 **원문 바이트를 먼저 검사**한다(space 또는 0x21–0x7E, `"`·`\` 제외). `strings.Fields`는
탭·개행도 구분자로 받아들이므로, 먼저 나누면 제어문자가 들어간 원문이 그대로 저장된다.

## 네트워크 어댑터

| 조건 | 오류 |
| --- | --- |
| provider/issuer/token endpoint가 https 아님·userinfo/query/fragment·2048자 초과 | `oauth.ErrExchange` (요청 시도 전) |
| 폼이 16KB 초과 | `ErrRequest` |
| doer nil·요청 실패·응답/본문 nil·읽기 실패·ctx 취소 | `ErrTransport` (원인 `%w` 보존) |
| 응답 `Request.URL`이 endpoint와 다름 | `ErrRedirect` |
| status ≠ 200 | `ErrStatus` (+ 분류된 `ErrTokenRejected`/`ErrTokenTransient`) |
| 200인데 Content-Type이 JSON 아님 | `ErrContentType` |
| 본문 64KB 초과 | `ErrTooLarge` |

**200이 아닌 응답은 절대 자격증명을 반환하지 않는다.** 본문이 성공 문서처럼 생겨도
`ErrStatus`다. 자격증명을 보내지 않고(`Authorization` 없음), 요청 본문은 오류 메시지에 넣지
않으며, 응답에서는 검증을 통과한 오류 코드만 인용한다.

**status 판정이 크기 판정보다 먼저다.** 401의 본문이 상한을 넘더라도 `ErrTooLarge` 대신
`ErrStatus`(+분류)를 반환한다. 호출자가 행동할 정보는 상태 코드이기 때문이다.

**폼과 endpoint는 분리할 수 없다.** `Exchange`는 `Provider`·`Client`·`Pending`·코드를 받아
내부에서 `BuildExchange`를 호출한다. 폼을 따로 받으면 한 발급자용 폼을 다른 호스트로 보내는
실수가 가능하고, 그 실수는 verifier 유출이다.

## 주장하지 않는 것

1. **refresh 호출** — `ApplyRefresh`는 응답을 받은 뒤의 판정만 한다. HTTP refresh는 미구현.
2. **재시도·백오프** — 없음. `ErrTokenTransient`는 분류일 뿐 스케줄러가 아니다.
3. **DCR** — client_id는 사전 등록 값.
4. **토큰 저장** — `Tokens`는 `MarshalJSON`을 거부하지만, 실제 보관은 Keychain/age 어댑터 몫.
5. **revoke 전송** — 미구현(`PlanLogout`은 계획만).
6. **TLS 검증** — `Doer`(*http.Client 기본값) 책임.

## 검증

```text
gofmt -l (변경 파일)                         출력 없음
go vet ./internal/auth/oauth/...             PASS
go test -count=1 ./internal/auth/oauth/...   PASS
go test -race -count=1 ...                   PASS
go build ./...                               PASS
```

변이로 방어선 고정을 확인했다(각 변이는 1줄, 실행 후 원복).

| 변이 | 실패한 테스트 |
| --- | --- |
| endpoint 동일성 검사 제거 | `TestExchangeRefusesRedirectedResponse` |
| non-200을 성공으로 처리 | `TestExchangeNeverAcceptsCredentialsFromANon200` |
| 200 Content-Type 검사 제거 | `TestExchangeRequiresJSONOnSuccess` |
| 본문 상한 제거 | `TestExchangeBoundsTheResponse` |
| issuer 일치 검사 제거 | `TestBuildExchangeRefusesMismatchedState` |
| verifier 형식 검사 제거 | `TestBuildExchangeRefusesMismatchedState` |
| error+access_token 모호성 검사 제거 | `TestParseTokenResponseRefusesAmbiguousErrorDocument` |
| `expires_in` 상한 제거 | `TestParseTokenResponseBoundsExpiresIn` |
| access_token `safeToken` 제거 | `TestParseTokenResponseRejectsUnusableCredentials` |
| `error` 코드 검증 제거 | `TestParseTokenErrorSanitisesHostileCodes` |
| `scope` 원문 바이트 검사 제거 | `TestParseTokenResponseRejectsMalformedScope` |
| issuer의 https 검사 제거 | `TestBuildExchangeRefusesMismatchedState/insecure_issuer` |
| 크기 판정을 status보다 먼저 | `TestExchangeKeepsTheStatusWhenAnErrorBodyIsOversized` |

실 제공자 **오류 경로** 1회 확인(임시 테스트, 커밋 안 함): 실제 Cloudflare 토큰 엔드포인트에
가짜 코드를 보내 `401 invalid_client` → `ErrStatus` + `ErrTokenRejected`, 자격증명 없음.
호출은 현재 시그니처로 `Exchange(ctx, &http.Client{}, provider, client, pending, "not-a-real-code", time.Now(), Policy{Timeout: 15 * time.Second})`.

```text
tokens_empty=true status_err=true rejected=true transient=false
err=… unexpected status: 401: … invalid_client
```

## 남는 미검증

- **성공 교환 실측** — 실제 대화형 로그인으로 받은 코드가 필요. **NOT VERIFIED**
- Linux/Flip3 exact-head 게이트 — **NOT VERIFIED**
- refresh·revoke·저장 어댑터 — 미구현(위 "주장하지 않는 것")
