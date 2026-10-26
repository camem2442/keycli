# HANDOFF — keycli (`kc`)

## 2026-10-26 — 공통 정책 채택

정책 lock을 `652dec3c`로 명시적으로 갱신했다. [의미 차이·검증 범위](docs/reviews/2026-10-26-workspace-policy-adoption.md) 참조. 제품/운영 인수와 별개인 로컬 문서 변경이다. NEXT CONTEXT: 기존 제품 작업 경계를 유지하며 다음 범위를 결정한다.

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**v0.6.0 출시 완료:** tag `v0.6.0` @ [`881758e`](https://github.com/camem2442/keycli/commit/881758ed2cd689204bb71ade8ad72a2fa19eeb5d) on `main`. PR [#7](https://github.com/camem2442/keycli/pull/7) merge, Linux `./verify.sh --suite all`·GitHub Actions on `main` green (2026-10-25). 릴리스 게이트: Cursor/gh 실 push(B-1)·clip `cp` 자동삭제 실측(KC-20)·OpenCode `kc exec --standalone`(KC-18) **COMPLETE**. OpenCode 백그라운드 서비스 경로·에이전트+정책 실사용 남음 — [roadmap](docs/planning/roadmap.md).

## 상태 (2026-10-25)

| 영역 | 상태 | 근거 |
|---|---|---|
| P0 릴리스 차단 7건 | **COMPLETE** | 구현·Linux `./verify.sh --suite all`·실 Mac mac-smoke·`exec` 노출(B-2)·clip `cp` 자동삭제 실측(KC-20, SJ Mac 2026-10-25) ([roadmap P0](docs/planning/roadmap.md)) |
| P1 계약·보안 정합성 9건 | **COMPLETE** | P1-1~P1-9 구현·Linux CI·mac-smoke ([roadmap P1](docs/planning/roadmap.md)) |
| P2 운영·문서 6건 | **COMPLETE** | `main` merge·Actions green·tag `v0.6.0` ([roadmap P2](docs/planning/roadmap.md)) |
| `kc wire opencode-vertex` | **COMPLETE** | PR #7 merge; `internal/opencode`, `internal/cli/wire.go`, CLI·agents 가이드; Mac `--dry-run`·Linux unit·CI |
| CLI M1–M5 | **PARTIAL** | P1 항목 반영 완료, 실 API NOT VERIFIED |
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
| 설치본 `~/.local/bin/kc` | **COMPLETE** | Mac: `v0.6.0` @ `881758e`, mac-smoke·wire `--dry-run` 통과 |
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

P0~P2·wire·Linux CI·실 Mac mac-smoke·`exec` 노출(B-2)은 끝났다. v0.6.0은 tag·release workflow까지 완료.

1. 릴리스 게이트: Cursor/gh push·clip 실측·OpenCode `kc exec --standalone` **COMPLETE**. 남음: 에이전트 실사용+정책 1회, OpenCode 백그라운드 서비스 경로.
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
