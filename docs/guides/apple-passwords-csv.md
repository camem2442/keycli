# Apple Passwords CSV로 주고받기

`kc`는 Apple Passwords 앱과 **CSV 파일**로만 비밀을 주고받는다. 앱에는 CLI·스크립팅
인터페이스가 없고(`sdef` -192), iCloud 웹 Passwords 앱도 없다. 두 방향 모두 평문
파일이 생기므로 명시 승인 플래그가 필요하다.

## 형식

| 열 | 내용 |
|---|---|
| `Title` | kc는 시크릿 이름을 쓴다 |
| `URL` | kc 내보내기에서는 빈 셀 |
| `Username` | kc 내보내기에서는 빈 셀(가져올 때는 note로 보존) |
| `Password` | 시크릿 값 |
| `Notes` | kc note |
| `OTPAuth` | Apple은 label 없는 `otpauth://totp?secret=…&algorithm=SHA1&digits=6&period=30` |

Apple은 모든 행에 6개 셀을 쓰고, 비어 있는 필드는 빈 셀로 나간다. kc의 CSV 코덱은
인용 필드 안의 CRLF를 보존한다(표준 `encoding/csv`는 LF로 바꾸고, CRLF 출력 모드에서는
홑 `\\r`을 버린다).

## kc → Apple Passwords

```bash
kc export github gitcoord_owner_ed25519 \
  --format csv --output-file /tmp/kc-passwords.csv --allow-plaintext

# Passwords 앱: File > Import Passwords from File > 파일 선택 > Import > Delete
```

- `--output-file` 필수, `0600`, 이미 있으면 덮어쓰지 않는다(`O_EXCL`).
- stdout·TTY 출력 없음. stderr에 행 수와 삭제 안내만 남는다.
- `--to`·`--to-github`·`--stdout`·`--verified-recipient`와 함께 쓸 수 없다.
- 값이 UTF-8이 아니면(바이너리 시크릿) 그 이름을 지목해 거부한다.
- URL이 비어 있으므로 웹사이트 자동완성에는 쓰이지 않는다. 사람이 읽고 복사하는 용도다.

가져온 뒤에는 파일을 지운다. Passwords 앱도 가져오기 직후 삭제 버튼을 제공한다.

## Apple Passwords → kc

```bash
# Passwords 앱: File > Export All Passwords to File > Passwords.csv
kc import --format csv --input-file ~/Downloads/Passwords.csv --allow-plaintext --dry-run
kc import --format csv --input-file ~/Downloads/Passwords.csv --allow-plaintext
```

- `--allow-plaintext` 없으면 거부한다. stdin도 받는다(`--input-file` 생략).
- `Title` → 이름: 소문자, `[a-z0-9._-]` 유지, 나머지는 `-`로 접는다. 같은 파일 안에서
  중복이면 `-2`, `-3`.
- `Password` → 값. `Username`·`URL` → note(`username=… | url=…`). Apple의 `Notes` 셀은
  **기본 미보관**이다 — `kc ls --json`으로 에이전트에 읽히고, 사람들이 복구 코드·seed를
  거기에 넣기 때문이다. 필요하면 `--keep-notes`로 명시한다.
- CSV는 설정된 로컬 file store에만 저장하며, 다른 백엔드로 우회하지 않는다.
- 이미 있는 이름은 `--force` 없이는 거부한다. `--force`도 계획에 포함된 충돌 이름만 대체한다. 이름은 쓰기 전에
  canonical로 해석하므로 alias(`gh` → `github`)도 충돌로 잡힌다. 파일 안 중복에서 생기는
  `-2` 접미사는 기존 이름과 겹치지 않을 때까지 올라간다.
- `--dry-run`은 이름과 `--force`가 필요한 이름만 보고하고, 충돌이 있어도 실패하지 않는다.
- 비밀번호가 빈 행은 건너뛰고 개수만 보고한다.
- `OTPAuth`는 **기본 미보관**이다. note 필드는 `kc ls --json`으로 에이전트에 읽히므로
  2FA seed를 거기에 넣지 않는다. `--keep-otp`를 주면 `NAME.otp` 시크릿으로 저장한다.
- `--dry-run`은 쓸 이름만 보고하고 아무것도 쓰지 않는다.

## 손실되는 것

| 항목 | 이유 |
|---|---|
| passkey | CSV에 실리지 않는다(앱이 내보내지 않음) |
| 삭제 대기 항목 | 내보내기에서 빠진다 |
| 항목당 두 번째 이후 URL | 첫 URL만 남는다 |
| Wi-Fi 비밀번호, 공유 그룹(내가 만든 그룹 제외) | 앱이 내보내지 않는다 |

## 검증 상태

- Linux CI: `internal/applecsv` 코덱·CLI 가드·왕복 가져오기 **PASS**(합성 값만).
- 실제 macOS Passwords 앱의 가져오기 오류 목록·자동완성·Touch ID 프롬프트: **NOT VERIFIED**.
