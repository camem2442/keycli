# 일회용 코드 (TOTP)

`kc`는 2FA seed를 **다른 시크릿과 같은 방식으로 보관**하고, 필요하면 코드를 계산한다.
계산기는 `internal/totp`(RFC 6238)이고, seed는 `kind=otp` 항목 하나에 담는다.

## 저장

```bash
# Apple Passwords CSV에서 (기본 미보관이므로 명시)
kc import --format csv --input-file Passwords.csv --allow-plaintext --keep-otp

# 직접: otpauth URI 또는 Base32 seed
kc set github_otp --kind otp        # 값 입력 프롬프트에 otpauth://totp?... 또는 seed
```

- 값은 `otpauth://totp/...` URI 또는 Base32 seed. URI가 아니면 SHA1·6자리·30초 기본값.
- `otpauth://hotp`는 거부한다(카운터 동기화는 지원하지 않는다).
- label 없는 Apple 표기(`otpauth://totp?secret=…`)도 받는다.
- seed는 note로 옮기지 않는다. note는 `kc ls --json`으로 읽히는 메타데이터다.
- 같은 이름을 다시 저장(교체)해도 `kind=otp`가 유지되고, 이때도 provider 검증을 건너뛴다.
  `--kind none`으로 미분류로 되돌릴 수 있다.

## 코드 얻기

| 방법 | 용도 | 비고 |
|---|---|---|
| `kc cp NAME --otp` | 사람이 붙여넣기 | 값 대신 현재 코드를 복사, 30초 뒤 자동 삭제 |
| `kc exec --otp NAME -- CMD` | 스크립트 | 자식 env `NAME_OTP`에만 주입. argv·로그 없음 |
| `kc otp NAME` | 터미널 확인 | **stdout이 TTY일 때만** 출력. 파이프는 `--stdout` 필요 |

```bash
kc cp github_otp --otp
kc exec --otp github_otp -- ./login.sh      # 자식 env: GITHUB_OTP
kc otp github_otp --at 2030-01-01T00:00:00Z  # 과거/미래 시점 계산(테스트·검증용)
```

환경변수 이름은 이름을 대문자로 올리고 비영숫자를 `_`로 바꾼 뒤 `_OTP`를 붙인다
(`github_otp` → `GITHUB_OTP`, `github_2fa` → `GITHUB_2FA_OTP`).

`kc exec`의 기본 전체 주입은 `kind=otp` 항목을 **건너뛴다**. seed가 자식 환경으로
새지 않게 하기 위해서다. 넣으려면 `--otp`로 코드만 넣거나 `--only`로 이름을 명시한다.

`kc otp`가 파이프에 그냥 출력하지 않는 이유는 같다. 에이전트 세션·CI 로그·`tee`에
6자리가 남으면 30초짜리 비밀번호가 영구 기록이 된다. 1Password CLI의 `op item get --otp`는
무조건 stdout으로 내보내므로, kc는 그 동작을 기본값으로 채택하지 않았다.

## 보안 경계

- 비밀번호와 TOTP seed를 **같은 저장소·같은 머신**에 두면 2요소가 사실상 1요소가 된다.
  기기 하나가 뚫리면 둘 다 뚫린다. kc는 편의를 위해 이 trade-off를 허용하지만,
  중요한 계정은 seed를 휴대폰 인증 앱에만 두는 편이 낫다.
- kc 정책(`[policy.agent.*]`)은 `KC_AGENT`로 우회 가능한 **가드레일**이지 경계가 아니다.
  `kind=otp` 항목은 `*_otp`·`*2fa*` deny 규칙으로 에이전트 읽기를 막는 것을 권한다.
- `kc audit`에는 명령 이름만 남고 코드·seed는 남지 않는다.
- `--at`은 결정적 테스트용이다. 실제 코드를 과거 시점으로 다시 만들 수 있다는 뜻이므로
  로그에 `--at`과 결과를 함께 남기지 않는다.

## 검증 상태

- RFC 6238 표준 벡터(SHA1/256/512), label 없는 URI, 기본값, HOTP 거부, 오류 메시지의
  seed 비노출, step 경계: `go test ./internal/totp` **PASS**.
- `kc otp`·`exec --otp`·`cp --otp`·seed 비주입: Linux 통합 테스트 **PASS**(합성 seed).
- 실제 provider 로그인 화면에 코드를 넣는 end-to-end: **NOT VERIFIED**.
- 자동 채움·주기 출력(S3)은 [계획](../planning/keycli-otp-s3-watch-20261126.md)에 이유와 함께
  보류로 기록했다.
