# HANDOFF — keycli (`kc`)

## 2026-11-26 — 시크릿 종류(`kind`)와 일회용 코드(S2)

시크릿에 **종류**를 붙였다: `kc set --kind login|token|key|otp|note`, 그리고
`--kind login`에서만 `--url`·`--username`. `kind`·`url`·`username`은 file store에만
저장되고 `kc ls --json`에 **값이 있을 때만** 실린다(스키마 v1의 하위 호환 추가). 미분류
항목은 지금까지와 똑같이 필드를 생략한다.

- Apple CSV 왕복이 좋아졌다: 가져오기는 `kind=login`·`url`·`username`을 전용 필드로
  저장하고(이전에는 note 문자열), 내보내기는 그 필드로 URL·Username 셀을 채운다.
  `--keep-otp`는 `kind=otp`인 `NAME.otp`로 저장하고, 내보낼 때 `NAME` 행의 `OTPAuth`
  셀로 합친다. 짝이 없는 seed는 이름 없이 개수만 보고한다.
- **일회용 코드(S2)**: `internal/totp`(RFC 6238, SHA1/256/512, 6–8자리, HOTP 거부)를
  추가하고 `kc otp NAME [--stdout] [--at RFC3339]`·`kc cp NAME --otp`·
  `kc exec --otp NAME -- CMD`를 붙였다. `kc otp`는 **stdout이 TTY일 때만** 출력하고
  파이프에는 `--stdout`을 요구한다(`op item get --otp`와 다른 선택). `kc exec`의 기본
  전체 주입은 `kind=otp` 항목을 건너뛴다 — seed가 자식 env로 새지 않게.
- 오류 메시지는 seed를 반복하지 않고, `totp.Config`는 `String`/`GoString`이 redacted다.
- 메타데이터 병합은 CLI(`internal/secretmeta`)에서 하고, 기준 레코드는 **쓰기 백엔드**가
  가진 것이다(env처럼 앞에서 이름을 가리는 읽기 전용 백엔드가 아님). file store는 받은
  값을 그대로 쓴다. 플래그 없이 같은 이름을 다시 저장(교체)해도 `kind`·`url`·`username`은 유지되고,
  `--url ""`처럼 명시한 빈 값은 지운다. `--kind none`은 미분류로 되돌린다. `login`이 아닌
  kind로 바꾸면 `url`·`username`은 지워진다. age 복원·마법사도 기존 분류를 보존한다.
- `kind=otp`는 새 저장이든 교체든 provider 검증을 건너뛴다(seed를 외부 API로 보내지 않음).
- `kc export NAME.otp --format csv`처럼 seed만 지정하면 0행 CSV를 성공으로 쓰지 않고,
  짝이 되는 로그인 이름을 안내하며 실패한다.
- S3(`--watch`, QR 재표시, 자동 채움)는 만들지 않고 이유·재검토 조건을
  [계획](docs/planning/keycli-otp-s3-watch-20261126.md)에 남겼다.
- 계약·가이드: [cli.md](docs/contracts/cli.md), [transfer.md](docs/contracts/transfer.md),
  [apple-passwords-csv.md](docs/guides/apple-passwords-csv.md),
  [one-time-codes.md](docs/guides/one-time-codes.md).
- **설치본 교체(2026-11-26)**: `~/.local/bin/kc`를 `v0.10.0(352e12d)`에서 이 브랜치
  최신 빌드(go1.22.12, `CGO_ENABLED=0`; 설치 시점 `7b65707`)로 바꿨다. 이전 바이너리는
  `~/.local/share/keycli-backups/kc-v0.10.0-352e12d-20261126.bin`에 두었다. 이전 커밋은
  현재 HEAD의 조상이라 후퇴가 아니며, `kc ls`/`status`/`doctor`와 저장소 파일이 교체 전후
  동일함을 확인했다. PR #104는 아직 열려 있으므로 이 설치본은 **미병합 브랜치 빌드**다.
- 검증: `internal/totp` RFC 벡터·파서 **PASS**, `internal/cli` **PASS**,
  `internal/integration`의 kind/CSV 왕복/`otp`/`exec --otp`/`cp --otp` **PASS**(합성 값).
  `verify.sh --suite docs`·`policy` **PASS**. `--suite unit`은 이 macOS에서 Go 1.22로 빌드한
  cgo 동적 바이너리가 `dyld: missing LC_UUID`로 죽어 실행 불가다 — `import "net"`만 있는
  최소 프로그램으로 재현했고 Go 1.27은 정상이라 환경 문제로 확정했다. 실행은 로컬 Go,
  gofmt·vet은 1.22로 확인했다.
- 추가 실측: TOTP를 독립 Python 구현과 218개 대조(불일치 0), 실제 pty/파이프 TTY 경계,
  독립 TOTP 검증기에 코드 제출(200)·오답 거부(401)·seed 비주입, 실제 로그인 키체인에서
  set/get/교체/ls/rm. macOS Passwords 앱은 가져오기 안내·파일 패널까지 확인했으나 실제
  가져오기는 세션 간섭으로 **BLOCKED**(사용자 볼트 무변경). 실제 provider 로그인·자동 채움:
  **NOT VERIFIED**.

## 2026-11-26 — Apple Passwords CSV 양방향

`kc export --format csv --output-file F --allow-plaintext`와
`kc import --format csv [--input-file F] --allow-plaintext [--dry-run] [--keep-otp]`를
추가했다. 근거: Passwords 앱은 CSV 가져오기만 제공하고 CLI·스크립팅 인터페이스가 없어
(`sdef` -192) 파일 경로만 가능하다. 형식은 `Title,URL,Username,Password,Notes,OTPAuth`
이고 Apple은 검증 코드를 label 없는 `otpauth://totp?secret=…`로 쓴다. 표준
`encoding/csv`는 인용 필드 안의 CRLF를 LF로 바꾸고 CRLF 출력 모드에서 홑 `\r`을 버려
바이트가 달라지므로 `internal/applecsv`에 보존 코덱을 두었다.

- 평문 출력은 `0600`·`O_EXCL`·stdout 금지, `--allow-plaintext` 없으면 거부.
- 가져오기는 기존 이름을 `--force` 없이 덮어쓰지 않고, 비밀번호 없는 행은 건너뛴다.
- `OTPAuth`는 기본 미보관(note 필드는 `ls --json`으로 읽히므로 seed를 넣지 않는다),
  `--keep-otp`면 `NAME.otp` 시크릿.
- 계약·가이드: [cli.md](docs/contracts/cli.md), [transfer.md](docs/contracts/transfer.md),
  [apple-passwords-csv.md](docs/guides/apple-passwords-csv.md).
- Linux CI: 코덱·CLI 가드·왕복 **PASS**(합성 값). 실제 Passwords 앱 가져오기·자동완성·
  Touch ID 프롬프트는 **NOT VERIFIED**.

## 2026-11-21 — GitCoord owner 서명 키 보관

GitCoord owner attestation용 Ed25519 키를 생성해 `kc set
gitcoord_owner_ed25519`로 보관했다(파일 `~/.config/gitcoord-owner/ed25519.pem`,
`0600`). key id `owner-camem2442-gitcoord-2026-11`, 공개키 지문
`sha256:2cf5ae70…`. 용도는 `workspace-infra/tooling/gitcoord_owner_attestation.py`
서명 하나뿐이며 API 토큰·SSH 키가 아니다. 어디 있는지·어떻게 쓰는지는
[보관 기록](docs/reviews/2026-11-21-gitcoord-owner-signing-key.md)이 정본이다.
Apple Passwords(iCloud) 사본도 2026-11-21에 CSV 가져오기로 만들었다 —
항목 `GitCoord owner attestation key`, 사용자 이름
`owner-camem2442-gitcoord-2026-11`, 메모에 PEM. 가져오기용 CSV는 즉시
삭제했고, 앱에는 CLI/스크립팅 인터페이스가 없어 이후 읽기는 사람만 할 수 있다.

## 2026-11-16 — 현재 계획 정본·PR 감사

현재 main `2897465f`는 #69로 v0.10 **소스만** 통합, [post-merge CI](https://github.com/camem2442/keycli/actions/runs/43695782808) 두 Job 성공. 정식 릴리스·Mac 설치 pin은 v0.8.0 유지. 열린 PR 13개와 main 531경로의 blob을 비교해 중복·역사적 검증·독립 변경을 분리했다. 현재 구현 격차와 G1~G6 의존성은 [2026-11-16 정본](docs/planning/keycli-current-roadmap-2026-11-16.md) 참조. 아래 2026-11-15 이하 원본 기록은 변경 없이 보존한다. 운영 효과/설치/CI dispatch 없음.

## 2026-11-15 — PR #69 소스 통합·R0 시작 회귀 수정

사용자는 #69 소스 병합과 두 저장소 main 조합의 합성 연동 확인을 승인했다.
Ready 후 Bugbot이 찾은 `cmd/kc-gateway-r0/main.go`의 `Lstat` 오류 변수 가림을
수정했다. 정상 신규 출력도 항상 거부되던 문제이며, 다섯 경로의 subprocess
시작 검사를 추가했다. 원본 `c2ffd8a3` + 새 테스트의 Linux ARM64 실행에서
정상 경로 실패를 재현했다. 수정본·정확 HEAD CI·병합 후 결과는
[PR #69](https://github.com/camem2442/keycli/pull/69)와 호스트 증거
`_management/evidence/keycli-pr69-main-integration-20261115/`가 소유한다.
테스트 대기·미실행은 성공이 아니다. 이 수정은 운영 인수나 릴리스가 아니며
실 Keychain/provider·설치 pin·M4.6 활성화·기존 PR/branch 삭제는 범위 밖이다.

## 2026-11-10 — v0.9/v0.10 소스 랜딩 후보 (MERGE_HOLD)

Draft PR #47~#63(검증 전용 #54/#57/#61 제외)의 고유 변경분만으로 후보 Tree
`ee735340`(`landing/keycli-v010-source-history-20261110` @ `80bdd82b`, 22커밋)를
재구성했다. `main 4c972d98` 기준이며 #61 CI Run `42787409731`(Go 1.22.12 linux/amd64,
`verify.sh --suite all`, 37 pkg ok)과 **코드 동등**이다. 유일한 차이는
`docs/guides/credential-rotation-lifecycle.md` 1건으로, 후보가 #56의 K10-04R 절을 보존한다.
`internal/rotation`(K10)은 아직 CLI에 배선되지 않아 사용자 표면 변화는
`kc delivery plan|status`, `kc auth probe`(offline)뿐이다.

- 상세·재현: [소스 랜딩 증거](docs/reviews/2026-11-10-v010-source-landing/README.md)
- 판정: `SOURCE_PARITY=PASS(문서 1건 drift)` · `INTEGRATION_CI=REUSED_VERIFIED(코드 동등)` ·
  `MAIN_LANDING=CANDIDATE_READY` · `TRUSTED_HOST=SOURCE_PARTIAL` ·
  `PROVIDER_LIVE=NOT_VERIFIED` · `RELEASE=BLOCKED`
- 블로커: PR 생성·병합·태그 권한(사용자 결정), mac-smoke 미실행, WI-KC-02 미구현
- 이번 작업의 실제 외부 효과: push/PR/merge/tag 0, provider mutation 0, Keychain·`~/.kc` 변경 0
## 2026-11-08 — 정책·전송 계층 마감

PR #30–#39로 `deny`, 거부 사유, `policy show|validate`, Cursor Cloud allowlist 파일럿, alias 실 PUT, `kc push cloudflare`, import 덮어쓰기 거부를 닫았다. 요약과 NOT VERIFIED 목록은 [마감 문서](docs/reviews/2026-11-08-policy-lifecycle-closeout.md)가 소유한다. 다음은 P5(검증 통합)와 P6(v0.8.0 제안)이다.

## 2026-11-08 — policy deny + 라이브 적용 검증

`[policy.agent.*] deny = [...]`를 추가했다(PR #30, `b69c3b6`). Cursor Cloud 단일 VM 파일럿은 릴리스 `v0.7.2`로 **allowlist 계약 PASS**(A/B/C = 0/5/2, 허용일 때만 자식 실행, 합성 시크릿만) — [영수증](docs/reviews/2026-11-08-kc-cursor-cloud-p3-01.md). `policy_digest` 재현·`pin_match`·`deny` 기능·실제 provider 자격증명은 **NOT VERIFIED**. `deny`는 `allow`보다 먼저 평가되고, 잘못된 글로브는 진단 명령(`doctor`/`policy`/`rules`)을 제외한 모든 명령에서 명시적으로 거부되며(PR #32) 평가 단계에서도 fail-closed다. Mac 로컬 `cursor` 기준으로 `get`/`exec`/`ls`/`policy check` 네 경로의 허용·거부·오류를 합성 probe로 확인했다([검증](docs/reviews/2026-11-08-policy-deny-live-enforcement.md)). 다중 에이전트 동시 적용·Cloud Agent·`kc push` 경로는 **NOT VERIFIED**이며, 이 정책은 보안 경계가 아닌 **실수 방지 가드레일**이다. `kc policy show`(적용 규칙·출처·강제 여부)와 `kc policy validate`(전체 패턴 사전 검증)를 추가했다. Cursor Cloud 별칭 경유 실 PUT → 목록 → DELETE 왕복은 합성 값으로 **COMPLETE**([영수증](docs/reviews/2026-11-08-kc-p4-cursor-alias-put.md)); `--repos` 범위·충돌·상한·GUI 주입은 **NOT VERIFIED**. `kc push cloudflare`는 일회성 Worker에 실 PUT 후 삭제까지 **COMPLETE**([영수증](docs/reviews/2026-11-08-kc-p4-cloudflare-push.md)); 범위·한도·롤백은 **NOT VERIFIED**. Editor 토큰은 자기 자신을 삭제할 수 없어 발급 주체로 회수해야 한다. 만료·회전·정리 소유권은 [문서](docs/reviews/2026-11-08-credential-lifecycle-ownership.md)로 고정했고, unguarded `kc import`는 기존 이름 덮어쓰기를 **거부**하며(`--force` 필요) 만료 없음 경고를 남긴다. 자동 갱신·전역 스윕은 비목표다.

## 2026-10-26 — 공통 정책 채택

정책 lock을 `652dec3c`로 명시적으로 갱신했다. [의미 차이·검증 범위](docs/reviews/2026-10-26-workspace-policy-adoption.md) 참조. 제품/운영 인수와 별개인 로컬 문서 변경이다. NEXT CONTEXT: 기존 제품 작업 경계를 유지하며 다음 범위를 결정한다.

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**v0.8.0 출시 범위 재정의 (2026-11-08):** v0.8.0은 모든 에이전트 운영 인수 버전이 아니라 **공통 인증 CLI 안정화 릴리스**다. 출시 차단 조건(정확한 소스 Linux CI·게시 자산 checksum·실 바이너리 합성 스모크·롤백)과 후속 **소비자 도입 조건**(에이전트별 1턴·Mac GUI/Keychain·공급자 전송)을 [v0.8.0 release gate](docs/planning/v080-release-gate.md)로 분리했다. 이전 로드맵 §4.E의 조건은 삭제하지 않고 대체 표시했으며, 소비자 조건의 현재 상태(NOT VERIFIED/PARTIAL)는 그대로다.

**v0.8.0 출시: COMPLETE** (릴리스 gate 범위만, [증거](docs/planning/v080-release-evidence.md)) — tag `v0.8.0` → `ad30607`, Linux CI·게시 checksum·Mac `kc-darwin-arm64` 합성 스모크 PASS. Mac `~/.local/bin/kc` 한 경로만 v0.8.0으로 교체, 이전 빌드는 `~/.local/libexec/kc-rollback/`에 보존. 로컬 설치 pin `v0.8.0`, Cloud pin `v0.7.2` 유지. 첫 소비자 파일럿은 Cursor local(감사 로그상 최다 사용): 지갑 자격증명을 에이전트 deny에 추가, 거부/누락/허용 합성 확인 PASS. 다음: Cursor 실제 작업 1턴 영수증, 운영 효과 토큰 범위 축소(CF cutover 종료 후), 나머지 에이전트는 필요 시 연결.

**v0.8.0 다중 에이전트 공통 인증 인수(소비자 도입): PARTIAL** (2026-11-07). 구현 정본과 운영 인수를 분리하고, [v08 로드맵](docs/planning/v08-multi-agent-auth-infrastructure.md)에 따라 MCP 안전 기본값·Mac TUI/DB 보호·Cursor/Claude/Codex 1턴·Cloudflare 실전 검증을 개별 Goal로 관리합니다. 기본 설치 pin은 **v0.7.2**이며, 릴리스의 Linux 바이너리 다운로드·SHA256·격리 재설치 MCP 인수는 **COMPLETE**([증거](docs/planning/v072-release-evidence.md))입니다. 실제 Mac GUI와 Codex/Claude 1턴 E2E, Cloudflare 운영 도입은 여전히 **NOT VERIFIED**입니다. v0.7.1 이하 바이너리의 MCP는 `get_secret`을 기본 노출하므로 공유 클라이언트에서 사용하지 않습니다.

### 이전 Goal — v0.7.0 운영 종결

**v0.7.0 출시 + operational close: COMPLETE** (2026-10-29 기준, [release gate](docs/planning/v07-release-gate.md)에 정의된 범위만). tag [`v0.7.0`](https://github.com/camem2442/keycli/releases/tag/v0.7.0). 운영 근거: [v07-operational-evidence.md](docs/planning/v07-operational-evidence.md).

- Mac mac-smoke·`kc push cursor` 실 PUT·목록·DELETE — **COMPLETE** (KC-55)
- Mac `kc push gh` (`camem2442/keycli`, `--app actions`) dry-run/set/delete — **COMPLETE** (KC-55); 다른 `--app`·`--user` 범위는 **NOT VERIFIED**
- Cloud Agent build·Activate·fresh VM smoke — **COMPLETE** (KC-40)
- `cursor_api_key` alias — 구현 **COMPLETE** (KC-57 `c14354d`); Mac alias 해석 + `--dry-run` **COMPLETE** (KC-59); alias 경유 실 PUT **NOT VERIFIED**
- OpenCode 서비스 경로(KC-22) — PR #11 CLOSED, [ADR 0006](docs/decisions/0006-opencode-execution-modes.md)로 `--standalone` 기본

실사용 증거: Mac Cursor·git push·OpenCode standalone, Cloud Agent VM. Codex/Claude 1턴은 **NOT VERIFIED**.

열린 일: KC-25(gh 401, 재현 시에만). 선택: Mac `/Users/k/bin/kc` 재설치, Codex/Claude E2E, infra Goal.

## 마일스톤 (M1–M6, 2026-10-29)

구현 vs 운영. `git-credential` 실 push ≠ `kc push cursor/gh` Secret API 전송.

| 마일스톤 | 구현 | 운영 성숙도 |
|----------|------|-------------|
| M1 CLI 코어 | **COMPLETE** | 실사용 가능 |
| M2 Keychain·Link | **COMPLETE** | Mac GUI 실사용 |
| M3 Clip·CP | **COMPLETE** | `kc cp` 실측(KC-20); export dotenv clip 경로는 integration만 |
| M4 Git·Rules·Doctor | **COMPLETE** | Git 실사용(B-1); 소비자별 인수 차이 |
| M4.5 Push·Export·Import | **PARTIAL** | Mac 실 전송 **COMPLETE**(KC-55: `push cursor`, `push gh --app actions`); gh 다른 범위·Cursor 팀 엔드포인트·alias 경유 실 PUT **NOT VERIFIED** |
| M5 External | **COMPLETE** | 프로토콜·Linux CI; 서드파티 플러그인 실연동 별개 |
| M6 Audit·Policy | **COMPLETE** | 드릴·CI; 강한 OS 격리 아님 |

## 상태 (2026-10-28)

| 영역 | 상태 | 근거 |
|---|---|---|
| P0 릴리스 차단 7건 | **COMPLETE** | 구현·Linux `./verify.sh --suite all`·실 Mac mac-smoke·`exec` 노출(B-2)·clip `cp` 자동삭제 실측(KC-20, SJ Mac 2026-10-25) ([roadmap P0](docs/planning/roadmap.md)) |
| P1 계약·보안 정합성 9건 | **COMPLETE** | P1-1~P1-9 구현·Linux CI·mac-smoke ([roadmap P1](docs/planning/roadmap.md)) |
| P2 운영·문서 6건 | **COMPLETE** | `main` merge·Actions green·tag `v0.6.0` ([roadmap P2](docs/planning/roadmap.md)) |
| `kc wire opencode-vertex` | **COMPLETE** | PR #7 merge; `internal/opencode`, `internal/cli/wire.go`, CLI·agents 가이드; Mac `--dry-run`·Linux unit·CI |
| M1–M6 (요약) | 위 표 | M4.5만 **PARTIAL** (push 범위 일부 미검증); 나머지 구현 **COMPLETE** |
| M6 audit + policy middleware | **COMPLETE** | Linux `./verify.sh --suite all`; 호출부 보정은 P1-2·P1-7 |
| git credential 호스트 매핑 (`[git."HOST"]`, `setup-git --host`) | **COMPLETE** | P1-1 반영, `c4b09ea` 커밋·mac-smoke·설치본 검증 통과 |
| `kc audit` / `kc policy check` | **COMPLETE** | 단위·integration |
| [0005-m6-audit-policy-middleware.md](docs/decisions/0005-m6-audit-policy-middleware.md) | **COMPLETE** | |
| mac-smoke (Darwin) | **COMPLETE** | 2026-10-25 실 Mac(GUI 로그인·keychain unlocked)에서 `KC="$PWD/dist/kc" bash scripts/mac-smoke.sh` 통과 |
| 샌드박스 종료 3 (실 Mac) | **COMPLETE** | `sandbox-exec` 안 `security` exit 44 + `... parameters ... not valid` → exit 3 매핑, 회귀 테스트 추가 |
| Cursor Cloud 실 push | **COMPLETE** | B-1(SJ Mac 2026-10-25): `origin.cursor.com`·`kc git-credential`만으로 `context-platform` push |
| gh 실 push | **COMPLETE** | B-1: `github.com`·`kc git-credential`만으로 `keycli` push (`setup-git`·`github` 시크릿) |
| 에이전트 실사용 + 정책 운영 | **NOT VERIFIED** | |
| OpenCode CLI turn (`kc wire` 후 실 `opencode`) | **PARTIAL** | KC-15/KC-18: `kc exec --only vertex-byok-proxy,PROXY_API_KEY -- opencode run --standalone` 1턴 OK; 백그라운드 `opencode run`(non-standalone)은 NOT VERIFIED |
| `exec` 환경 키체인 항목 노출 | **COMPLETE** | B-2(SJ Mac 2026-10-25): throwaway `kc.secret.*`, `kc exec -- env`에서 시크릿·`KC_*` 누출 없음 |
| clip 자동삭제 실측 | **COMPLETE** | KC-20: `KC_CP_CLEAR_SECONDS=4`·throwaway `kc cp` 후 delay 경과 시 클립보드에 시크릿 미유지 |
| 설치본 `~/.local/bin/kc` | **COMPLETE** | Mac: `KC_INSTALL_REF=v0.7.0 bash scripts/install.sh` (sha256 verified, KC-55 2026-10-28). `/Users/k/bin/kc`는 KC-57 이전 빌드일 수 있음 — 재설치는 선택 작업 |
| v0.7.0 operational close | **COMPLETE** | KC-55(Mac)·KC-40(Cloud)·KC-57/59(alias, dry-run) — [v07-operational-evidence.md](docs/planning/v07-operational-evidence.md) |
| TTL 캐시 / rate limit / masking / 1Password | **DEFERRED** | roadmap |

## 검증

```bash
# Linux
./verify.sh --suite all

# macOS 단위/integration (태그 필수)
go test -tags=kc_testhooks ./...
./verify.sh --suite mac-smoke   # P0-2 이후, 실 Mac
```

태그 없는 `go test ./...`는 클립보드 테스트를 컴파일하지 않으므로 안전하다. 실제 클립보드 경로를 검증하려면 `-tags=kc_testhooks`로 실행한다(가짜 clipboard 사용).

## NEXT CONTEXT

v0.7.0 [GitHub Release](https://github.com/camem2442/keycli/releases/tag/v0.7.0)과 operational close(KC-55·KC-40·KC-57/59)까지 완료.

1. v0.7 이후 남은 일: KC-25(gh 401, 재현 시), Codex/Claude 1턴 E2E(**NOT VERIFIED**), 에이전트+정책 1회, alias 경유 실 PUT·gh 다른 `--app` 범위(**NOT VERIFIED**). 선택: Mac `/Users/k/bin/kc` 재설치, infra Goal. OpenCode 백그라운드 서비스는 ADR 0006으로 기본 경로에서 제외.
2. 실 Mac mac-smoke는 2026-10-25 통과했다(keychain 쓰기/읽기·rebuild 읽기·`ls --json`의 `kc.secret.*`·ai-core keychain 읽기·`kc.age` 왕복·link·setup-git·샌드박스 종료 3). 재실행 시에도 **SSH 세션에서는 불가**: `launchctl managername=Background` + 잠긴 login keychain 때문에 `security -i`가 `-25308 User interaction is not allowed`로 실패한다. Mac의 GUI 로그인 세션에서 keychain을 해제한 뒤 `KC="$PWD/dist/kc" bash scripts/mac-smoke.sh`로 실행한다.
3. P2-6: **COMPLETE** — `c4b09ea`(코드)·`39a19c3`(문서), 설치본 `~/.local/bin/kc` 재빌드·mac-smoke 통과. `main` @ `v0.6.0` 설치본은 `bash scripts/lifecycle.sh build-dev --install` 또는 release 바이너리로 맞춘다.
P1/P2에서 바뀐 동작(스크립트 주의):

- `ls`는 기본으로 값을 읽지 않는다. stale 표시는 `kc ls --stale`(원장에 있는 이름만 읽음).
- `exec` 자식 환경에서 `KC_KEY`·`KC_AGENT`를 제거한다.
- `set --clip`은 저장 성공 후에만 클립보드를 비운다. 파이프 입력의 끝 개행 1개는 제거된다.
- `github.com`은 config에 없어도 암묵 기본으로 동작하되 시크릿이 없으면 무응답. `setup-git`은 매핑을 명시적으로 기록한다.
- keychain 쓰기는 항상 `kc.secret.<name>`, `kc.age`는 예약.
- 정책은 `middleware = [..., "policy"]`가 있을 때만 `ls` 표시와 `get` 거부에 함께 적용된다.
- `doctor --json`의 `kc_age`는 identity 출처(`env`/`file`/`keychain`/`missing`/`locked`/`blocked`), `identity_path`는 file일 때만.
- `scripts/install.sh`는 release 바이너리를 `SHA256SUMS`와 대조한 뒤에만 설치한다.
- `push`는 원장을 한 번에 기록한다(`ledger.RecordAll`).

주의: `kc init`은 이제 기존 identity(`KC_KEY` → `~/.kc/receiver.key` → `kc.age`)의 recipient만 출력한다. 스크립트에서 identity 자체가 필요하면 `kc init --print-identity`를 쓰고 출력을 로그에 남기지 않는다.

## Cursor wire safety repair candidate — 2026-10-31

Base main 1f0224e contains the original PR #14 Cursor wire bytes. This separate repair propagates process-observation errors (only pgrep exit 1 is normal no-match), DB read failures and failed quit requests; SQL INSERT uses JSON encoding and reads open SQLite read-only. Invalid URL structures are rejected and Updated is set only after a successful write. Regression cases cover missing-row JSON escaping, corrupt DB preservation, process observation errors/timeouts and URL validation. No operating Cursor DB, process, installed binary or credentials were modified.

Local Go compilation/gofmt/owning verifier are NOT RUN: this review executor has no functioning Go toolchain or sqlite3 CLI. Earlier protected Python SQLite reproduction demonstrated the original JSON defect but is not Go test evidence. The existing PR workflow is the next verification path; its exact-head result and SQLite-dependent skips must be checked before merge. Original PR #14 and local evidence remain preserved.
