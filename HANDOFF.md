# HANDOFF — keycli (`kc`)

## 2026-10-26 — 공통 정책 채택

정책 lock을 `652dec3c`로 명시적으로 갱신했다. [의미 차이·검증 범위](docs/reviews/2026-10-26-workspace-policy-adoption.md) 참조. 제품/운영 인수와 별개인 로컬 문서 변경이다. NEXT CONTEXT: 기존 제품 작업 경계를 유지하며 다음 범위를 결정한다.

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

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
