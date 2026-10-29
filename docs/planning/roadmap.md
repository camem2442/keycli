---
status: active
---
# 로드맵

기준: 2026-10-28 (v0.6.0 출시·V07-A1 문서 정합). 상태어는 **COMPLETE / PARTIAL / BLOCKED / DEFERRED / NOT VERIFIED**만 사용한다.

우선순위 원칙:

1. **사용자 데이터·전송 경로**의 정합성 (키 생성/복구, 잠금, 클립보드).
2. **실제 macOS에서 확인되지 않은 경로**를 COMPLETE로 두지 않는다.
3. **CLI 계약**(출력·종료 코드)을 먼저 고정하고 기능을 얹는다.
4. 보안·동시성은 기능 추가보다 앞선다. 문서·CI는 마지막에 정리한다.

> 릴리스 **v0.6.0** 출시: PR [#7](https://github.com/camem2442/keycli/pull/7) merge·tag `v0.6.0` @ `881758e` on `main` (2026-10-25). P0~P2 + `kc wire opencode-vertex` 포함.

## P0 — 릴리스 차단

| ID | 작업 | 근거 | 완료 조건 | 상태 |
|---|---|---|---|---|
| P0-1 | `init`/`keygen`이 기존 identity의 recipient를 출력 | `internal/cli/init.go:20-44` — 매번 새 키를 만들고, `receiver.key`가 있으면 저장은 건너뛰고 새 recipient를 출력. 연속 실행 시 다른 `age1…` 확인. README·mac-smoke가 recipient를 identity로 사용 | `KC_KEY` → `receiver.key` → `kc.age` 순으로 identity를 찾아 그 recipient만 출력. 연속 실행 동일 + export/import 왕복 테스트 | COMPLETE |
| P0-2 | 실 macOS 키체인 목록 파서 | `internal/backend/keychain/keychain.go:136`, `aicore.go:96`은 `service:`만 파싱. 실제 `security dump-keychain`은 `"svce"<blob>=` 형식이며 `cmd/fakesecurity`가 이를 가림. macOS에서 `ls`/`exec`/`cp` 목록에서 키체인 항목 누락 | 실제 dump 형식 파싱 + fake를 실제 형식으로 갱신 + 실 Mac에서 `ls --json`·`exec` 포함 확인 | COMPLETE |
| P0-3 | 잠김 저장소를 `ls`/`exec`에서 오류로 전파 | `internal/resolve/resolve.go:212`가 `ErrLocked`를 무시. 잠김 상태에서 `ls`는 exit 0 + 빈 목록, `exec`는 시크릿 없이 자식 실행 | 잠김/차단 시 exit 4(또는 명시적 경고) + `exec` 자식 미실행. 단위·integration 테스트 | COMPLETE |
| P0-4 | `export --format dotenv` 자동 삭제 | `internal/cli/export.go:70`의 `CopyWithAutoClear` goroutine이 프로세스 종료와 함께 사라짐. `cp`만 `_clip-wait-clear` 자식 사용 | export 종료 후 delay 뒤 클립보드 비움(그새 변경 시 유지). 통합 테스트 | COMPLETE |
| P0-5 | external 백엔드 실패 격리 + 읽기 경로 네트워크 차단 | `internal/app/app.go:50`, `internal/backend/external/external.go:69` — 깨진 플러그인 하나로 `ls`·`doctor`까지 exit 1. `Deps.NetworkBlocked` 미연결, `networkSkip()` 항상 false | 깨진 플러그인이 있어도 `doctor`/`plugin list`/다른 백엔드 동작. 읽기 경로에서 네트워크 backend skip | COMPLETE |
| P0-6 | env 백엔드 목록/조회 대칭 | `internal/backend/env/env.go:84`가 `KC_*`를 `key`·`agent`·`config`로 노출하지만 `get`은 불가. 임의 `MY_TOKEN`은 읽히지만 목록엔 없음 | `ls`의 모든 이름이 `get` 가능(또는 내부 변수 제외) + 테스트 | COMPLETE |
| P0-7 | 종료 코드 계약 준수 | `internal/cli/root.go:68`의 완전 일치 비교로 unknown command/flag/인자 부족이 exit 1(계약 64). 첫 실행 `set`(키 없음)은 exit 1(계약 4) | usage 64, 잠김/키 없음 4 + 안내 문구. 표 기반 CLI 테스트 | COMPLETE |

### P0 구현 기록 (2026-10-25)

- **P0-1:** `internal/backend/file/identity.go`의 `ResolveIdentity`/`SaveIdentityFile`로 키 해석을 공유(`KC_KEY` → `receiver.key` → `kc.age`). `kc init`은 기존 identity의 recipient만 출력하고 없을 때만 새로 저장하며, `--print-identity`를 추가했다. README·mac-smoke를 identity 기준으로 수정. 연속 `init` 동일성·`--print-identity`·export/import 왕복 integration 통과.
- **P0-2:** `security dump-keychain`의 `"svce"<blob>="…"`/`"acct"` 형식과 octal escape를 파싱하는 `ParseDumpEntries` 추가. `cmd/fakesecurity`를 실제 형식(`class: "genp"`, `svce`, `acct`)으로 갱신. 실 Mac dump 184개 `svce` 중 non-NULL 172개 파싱 확인(ai-core 6). 실 Mac mac-smoke에서 `ls --json`의 `kc.secret.kc.test.*` 노출 확인. `exec` 환경 키체인 노출은 B-2(SJ Mac 2026-10-25) **COMPLETE**.
- **P0-3:** `Resolver.List`가 `Available`의 비차단 오류(특히 `ErrLocked`)를 전파하고, `exec`는 잠김/차단 시 자식을 실행하지 않는다. 단위·integration 통과.
- **P0-4:** `export --format dotenv`도 `cp`와 같은 분리 자식(`_clip-wait-clear`)으로 자동 삭제를 예약한다. 변경된 클립보드는 유지하는 integration 통과.
- **P0-5:** external 백엔드의 plugin 탐색·handshake 실패를 `Available`로 지연해 `Build`를 깨뜨리지 않는다. resolver 읽기 경로는 `NeedsNetwork` 백엔드를 건너뛴다. 깨진 plugin이 있어도 `ls`/`get`/`doctor`/`plugin list`가 동작하는 integration 통과.
- **P0-6:** env `ls`에서 `KC_*` 내부 변수를 제외해 목록의 모든 이름이 resolver로 조회 가능하게 했다. 별칭 env 테스트·integration 통과.
- **P0-7:** cobra 사용법 오류를 64로, 첫 `set`(identity 없음)을 4로 매핑. 표 기반 integration 통과.

검증: macOS에서 `go test -tags=kc_testhooks ./...`, `verify.sh --suite unit|docs|policy`, 실 Mac `mac-smoke`·B-2 `exec` 노출 통과. Linux `./verify.sh --suite all`·GitHub Actions on `main` **COMPLETE** (2026-10-25). `kc cp` clip 자동삭제 실측(KC-20) **COMPLETE**; `export --format dotenv` clip 경로는 integration·mac-smoke 미포함.

## P1 — 계약·보안 정합성

| ID | 작업 | 근거 | 완료 조건 | 상태 |
|---|---|---|---|---|
| P1-1 | git 기본 매핑과 "무응답" 계약 정리 | `internal/config/config.go:133`가 `github.com`을 항상 주입. `github` 시크릿이 없으면 `git-credential get`이 exit 2. 문서는 "매핑 없으면 무응답" | 명시 매핑만 강제, 자동 기본은 not-found 시 무응답. `setup-git`이 매핑을 명시적으로 기록 | COMPLETE |
| P1-2 | stale 표시·audit 컨텍스트 정리 | `internal/cli/ls.go:24` → `internal/app/stale.go:17`이 모든 시크릿을 Get. N개면 N회 복호화·N줄 audit(`command=""`), 정책 거부 이름도 읽기 시도 | 배치 stale(또는 `--stale` 옵션), audit command 채움, 거부 이름 미읽기. audit 테스트 | COMPLETE |
| P1-3 | 자식 프로세스로의 비밀 노출면 축소 | `internal/cli/exec.go:38,58`이 `KC_KEY`/`KC_AGENT`를 자식에 전달. `internal/cli/cp.go:54`가 시크릿 SHA-256을 argv로 전달 | exec에서 `KC_KEY`/`KC_AGENT` 제거(또는 opt-in), checksum은 stdin/0600 임시파일 | COMPLETE |
| P1-4 | `set` 입력·클립보드 안전성 | stdin 끝 개행 미제거(`echo secret` 후 `kc set` → 이중 개행). `set --clip`은 저장 실패에도 `Clear` 실행 | 끝 개행 1개 제거, 저장 성공 후에만 클립보드 Clear. CLI 테스트 | COMPLETE |
| P1-5 | 키체인 이름 규칙·`security -i` 안전성 | `internal/backend/keychain/keychain.go:78`이 점 포함 이름을 서비스명으로 사용(`ls` 누락), `kc.age` 덮어쓰기·삭제 가능, 공백/개행 미검증 | bare name은 `kc.secret.` 강제, `kc.age` 예약, 이름 정규식, 특수문자 테스트 | COMPLETE |
| P1-6 | 파일 저장소·원장 cross-process 잠금 | `internal/backend/file/file.go:172,217`은 읽기 inode/임시파일만 잠금. read-modify-write 경쟁, `LedgerHMACKey`도 경쟁 | 전용 lock 파일로 RMW 직렬화. 동시 `set` 테스트 | COMPLETE |
| P1-7 | 정책 표시와 강제 일치 | `internal/resolve/resolve.go:253`은 rules만 있으면 `(policy)` 표시, `internal/middleware/build.go:12`는 `middleware`에 없으면 미강제 | `ls` 표시와 `get` 강제가 같은 기준. 문서화 | COMPLETE |
| P1-8 | doctor/Reload 정합성 | `doctor`가 `receiver.key`를 무시해 `kc_age: missing`으로 오표시. `App.Reload`가 오래된 `Deps.Config` 사용 | doctor가 키 출처 3종 표시, Reload 후 새 링크 반영 테스트 | COMPLETE |
| P1-9 | CLI 계약 세부 정합 | `ls --json` `omitempty`로 false 필드 누락, `get --source` link prefix, `status` 오타·조용한 skip, `audit --json` null | 계약 예시와 일치 + 회귀 테스트 | COMPLETE |

### P1 구현 기록 (2026-10-25)

- **P1-1:** `github.com` 자동 기본은 config에 쓰지 않고 `GitHost`의 암묵적 fallback(`Implicit`)으로만 둔다. 암묵 기본의 시크릿이 없으면 `git-credential get`은 무응답, 명시 매핑은 종료 2. `setup-git`은 대상 호스트의 `[git."HOST"]`를 명시적으로 기록한다. config·integration 테스트 통과.
- **P1-2:** `ls --stale` opt-in. 원장에 있는 이름만 읽고, 정책 거부 이름은 읽지 않으며, 원장은 한 번만 로드한다. audit `command`는 `ls`. 감사 로그 테스트 통과.
- **P1-3:** `exec` 자식 환경에서 `KC_KEY`/`KC_AGENT`를 제거. `_clip-wait-clear` checksum은 argv 대신 stdin 파이프로 전달. integration 통과.
- **P1-4:** 파이프 입력의 끝 개행 1개(LF/CRLF)만 제거. `set --clip`은 저장 성공 후에만 클립보드를 비운다. integration 통과.
- **P1-5:** keychain 쓰기는 항상 `kc.secret.<name>`. 이름 정규식 `[A-Za-z0-9][A-Za-z0-9._:#-]*`, `kc.age` 예약, `ls`는 유효 이름만 표시. 특수문자·예약 이름 테스트 통과. 실 Mac mac-smoke에서 샌드박스 종료 3 검사가 exit 2로 새는 결함을 발견: 샌드박스 안 `security`는 exit 44 + `SecKeychainSearchCreateFromAttributes: ... not valid`를 내므로, 해당 stderr를 `ErrBlocked`(종료 3)로 매핑하고 `errors_test.go` 회귀 테스트 추가.
- **P1-6:** store와 ledger의 read-modify-write를 전용 `.lock` 파일 flock으로 직렬화. 동시 `set`·동시 ledger `Record` 테스트 통과.
- **P1-7:** `middleware.PolicyEnabled`를 단일 기준으로 `ls` 표시·`get` 강제·`policy check`를 일치시킨다. 규칙만 있고 미들웨어가 없으면 미강제. integration 통과.
- **P1-8:** `doctor`가 `KC_KEY`/`receiver.key`/`kc.age` 출처를 `kc_age`와 `identity_path`로 표시. `App.Reload`가 `Deps.Config`를 갱신해 새 링크를 반영. 단위·integration 통과.
- **P1-9:** `ls --json`의 `stale_export`/`policy_denied` false를 항상 출력. `audit --json`은 빈 결과에 `[]`. `status` 오타 수정 + 읽기 실패 이름을 값 없이 stderr에 보고. `get --source`의 `link:` prefix 회귀 유지. 통과.

검증: macOS에서 `go test -tags=kc_testhooks ./...`, `verify.sh --suite unit|docs|policy`, `GOOS=linux/darwin go vet ./...`, 실 Mac `mac-smoke`(keychain 쓰기/읽기, rebuild 읽기, `ls --json`의 `kc.secret.*`, ai-core keychain 읽기, `kc.age` 왕복, link 읽기, setup-git, 샌드박스 종료 3) 통과. Linux `./verify.sh --suite all`·GitHub Actions on `main` **COMPLETE** (2026-10-25).

## P2 — 운영·문서

| ID | 작업 | 완료 조건 | 상태 |
|---|---|---|---|
| P2-1 | 테스트 CI | Linux에서 `go test -tags=kc_testhooks ./...` + `go vet` 자동 실행. Darwin은 수동/mac-smoke | COMPLETE |
| P2-2 | 설치 무결성 | `scripts/install.sh`가 릴리스 `SHA256SUMS` 검증 | COMPLETE |
| P2-3 | 문서 동기화 | README(`v0.6.0`), HANDOFF, roadmap, mac-smoke의 identity 사용법 일치 | COMPLETE |
| P2-4 | 테스트 위생 | 태그 없는 `go test`가 실제 클립보드를 건드리지 않도록 clip 테스트 gate | COMPLETE |
| P2-5 | push/ledger 성능 | `ledger.Record`를 항목별 호출하지 않고 일괄 기록 | COMPLETE |
| P2-6 | 미커밋 변경 정리 | git 호스트 매핑 변경 커밋 + 설치본 재빌드 | COMPLETE |

### P2 구현 기록 (2026-10-25)

- **P2-1:** `.github/workflows/test.yml` 추가. `main` push·PR·수동 실행에서 Linux `./verify.sh --suite all`(gofmt·vet·vet_darwin·unit·release·mac-smoke skip)을 돌린다. PR #7 merge 후 `main` workflow green — **COMPLETE** (2026-10-25).
- **P2-2:** `scripts/install.sh`의 release 경로(curl·`gh release download`)가 `SHA256SUMS`를 내려받아 `sha256sum`/`shasum -a 256`으로 대조한 뒤에만 설치한다. 합계 파일 부재·불일치는 실패(go install로 fallback). `scripts/lifecycle.sh release`도 macOS에서 `shasum`으로 `SHA256SUMS`를 만들도록 보정. 로컬에서 정상/불일치/항목 누락 3케이스와 `--suite release` 통과.
- **P2-3:** README 상태를 `v0.6.0`으로, 설치 절에 SHA256 검증, 테스트 절에 clip gate를 반영. HANDOFF·roadmap에 v0.6.0 출시·검증 상태 동기화. mac-smoke는 `kc.secret.<name>` 서비스·`ls --json` 노출을 실제로 확인하도록 수정.
- **P2-4:** `internal/clip/autoclear_test.go`에 `//go:build kc_testhooks`를 붙여 태그 없는 `go test ./...`가 실제 클립보드를 건드리지 않게 했다. 고정 sleep 대신 polling으로 flake도 제거.
- **P2-5:** `ledger.RecordAll` 추가. `push`는 시크릿 N개를 한 번의 load/save로 기록한다. 단일 `Record`는 `RecordAll`에 위임. batch upsert·빈 입력 테스트 추가.
- **P2-6:** 미커밋 변경(git 호스트 매핑+P0/P1/P2)을 커밋 `c4b09ea`로 정리하고 `scripts/lifecycle.sh build-dev --install`로 설치본을 재빌드했다. 설치본 `~/.local/bin/kc`(`v0.5.0-1-gc4b09ea`)로 실 Mac mac-smoke를 재실행해 통과.

검증: `go test -tags=kc_testhooks ./internal/ledger ./internal/clip ./internal/integration`, 태그 없는 `go test ./internal/clip ./internal/ledger`, `bash -n scripts/install.sh scripts/mac-smoke.sh`, install checksum 3케이스, `verify.sh --suite unit|docs|policy|release`, 실 Mac `mac-smoke` 통과. Linux VM·GitHub Actions on `main` `./verify.sh --suite all` **COMPLETE** (2026-10-25, `881758e`).

## 릴리스 게이트 (v0.6.0)

- P0~P2 구현 — Linux VM `verify.sh --suite all` + 실 Mac mac-smoke **COMPLETE**
- `kc wire opencode-vertex` — **COMPLETE** (PR [#7](https://github.com/camem2442/keycli/pull/7), `881758e`)
- `main` merge + `.github/workflows/test.yml` green — **COMPLETE** (2026-10-25)
- git tag `v0.6.0` @ `881758e` — **COMPLETE** (release workflow green)
- 실 Mac `./verify.sh --suite mac-smoke` — **COMPLETE** (2026-10-25)
- `exec` 키체인 노출 — **COMPLETE** (B-2, SJ Mac 2026-10-25)
- Cursor/gh 실 push 각 1회 — **COMPLETE** (B-1, SJ Mac 2026-10-25, `kc git-credential`)
- 에이전트 실사용 + 정책 운영 — **NOT VERIFIED**
- OpenCode CLI turn (`wire` 후 실 `opencode`) — **PARTIAL** (KC-18: `kc exec`+`--standalone` OK; 백그라운드 서비스 non-standalone NOT VERIFIED)
- clip 자동삭제 실측 — **COMPLETE** (KC-20, SJ Mac `kc cp`+delay)

릴리스: **COMPLETE** — [v0.6.0](https://github.com/camem2442/keycli/releases/tag/v0.6.0) @ `881758e` (2026-10-25). v0.7.0: **COMPLETE** — 출시 + operational close([v07-release-gate.md](v07-release-gate.md)). 남은 인수: 에이전트+정책·Codex/Claude 1턴·`kc push gh` Mac **NOT VERIFIED**; OpenCode 서비스 경로는 ADR 0006으로 opt-in 제외.

## 마일스톤 기록

| 단계 | 내용 | 원래 종료 조건 |
|---|---|---|
| M1 | cobra 골격, config, resolver, env·file 백엔드, `ls/get/exec/set/rm`, 종료 코드 | Linux `verify.sh --suite unit` |
| M2 | keychain 백엔드(`security`), `link`, ai-core 읽기 어댑터 | macOS 수동 체크리스트 + 가짜 구현 테스트 |
| M3 | 클립보드 `cp`/`set --clip`, OSC 52, 자동 삭제 | 맥·SSH 수동 확인 |
| M4 | `setup-git`, `rules`, `doctor`, 에이전트별 설정 가이드 | Cursor/Claude Code/Codex/Antigravity 각 1회 실사용 |
| M4.5 | `push cursor`, `push gh`, `export --format dotenv`, age `export/import`, 원장·`status` | Linux `verify.sh --suite all`; `git-credential` 실 push(B-1) **COMPLETE**; `kc push cursor` Mac **COMPLETE**(KC-55); `kc push gh` Mac NOT VERIFIED |
| M5 | 외부 하위 명령·백엔드 프로토콜 | `kc plugin list`, 예제 백엔드, Linux `verify.sh --suite all` |
| M6 | 감사·정책 미들웨어, `kc audit`, `kc policy check` | Linux `verify.sh --suite all` |
| 이후 | TTL 캐시, rate limit, 출력 가림, 1Password 백엔드 | DEFERRED |

## 현재 판정 (2026-10-28)

| 마일스톤 | 구현 | 운영 성숙도 |
|---|---|---|
| M1 CLI 코어 | **COMPLETE** | P0-6·P0-7, P1-3·P1-4·P1-6, Linux CI; 실사용 가능 |
| M2 keychain·link | **COMPLETE** | P0-2·P1-5, mac-smoke; Mac GUI 실사용 |
| M3 clip/cp | **COMPLETE** | P0-4·P1-4·KC-20 `cp` 실측; export dotenv clip은 integration |
| M4 setup-git·rules·doctor | **COMPLETE** | P1-1·P1-8, B-1 git push; 소비자별 인수 차이 |
| M4.5 push/export/import/status | **PARTIAL** | P1-9, P2-5; `kc push` Secret API E2E **NOT VERIFIED** |
| M5 external | **COMPLETE** | P0-5, Linux CI; 서드파티 플러그인 실연동 별개 |
| M6 audit·policy | **COMPLETE** | P1-2·P1-7; 드릴·CI, 강한 OS 격리 아님 |
| `kc wire opencode-vertex` | **COMPLETE** | PR #7, `881758e`, Linux unit·Mac `--dry-run` |
| git credential 호스트 매핑 | **COMPLETE** | P1-1, `c4b09ea`, mac-smoke |
| P2 운영·문서 | **COMPLETE** | CI green on `main`, 설치 검증·clip gate·ledger batch·v0.6.0 문서 |
| mac-smoke (Darwin) | **COMPLETE** | 2026-10-25 실 Mac(GUI 로그인, keychain unlocked)에서 `KC="$PWD/dist/kc" bash scripts/mac-smoke.sh` 통과 |

## 검증 대기 (NOT VERIFIED)

| 항목 | 비고 |
|---|---|
| `kc push cursor` / `kc push gh` Secret API | M4.5; `git-credential` push(B-1)와 독립 |
| `export --format dotenv` clip 자동삭제 실측 | integration만; mac-smoke 미포함 |
| 에이전트 실사용 + 정책 운영 | Cursor/Claude Code/Codex 각 1회 |
| OpenCode `kc exec` 경로 | `PROXY_API_KEY`를 kc store·`kc exec`로 주입 후 `opencode run`(non-standalone) |
| 서드파티 `kc-backend-*` 실연동 | demo 외 |

## DEFERRED

TTL 캐시, rate limit, 출력 가림, 1Password 백엔드, 외부 `kc-target-*`.
