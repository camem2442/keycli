# HANDOFF — keycli (`kc`)

## Product Goal

[README.md](README.md) 참조.

## Current Work Goal

**P0·P1·P2 구현·실 Mac 검증 완료:** [roadmap](docs/planning/roadmap.md)의 P0-1~P0-7, P1-1~P1-9, P2-1~P2-6을 구현했고 로컬 테스트와 실 Mac `mac-smoke`를 통과했다. `c4b09ea`·`39a19c3` 커밋 및 설치본 재검증까지 끝났다. 남은 일은 Linux CI(`./verify.sh --suite all`)와 릴리스 게이트(원격 push 후 CI, Cursor/gh 실 push, 에이전트 실사용, 클립보드·`exec` 미검증 항목)이다.

## 상태 (2026-10-25)

| 영역 | 상태 | 근거 |
|---|---|---|
| P0 릴리스 차단 7건 | **PARTIAL** | 구현·로컬 `go test -tags=kc_testhooks ./...`·실 Mac mac-smoke 통과. Linux CI 대기, `exec` 키체인 노출 NOT VERIFIED ([roadmap P0](docs/planning/roadmap.md)) |
| P1 계약·보안 정합성 9건 | **PARTIAL** | P1-1~P1-9 구현·로컬 unit/integration·mac-smoke 통과. Linux CI 대기 ([roadmap P1](docs/planning/roadmap.md)) |
| P2 운영·문서 6건 | **PARTIAL** | CI 워크플로·설치 SHA256·clip gate·ledger batch·문서 동기화 구현, 실 Mac mac-smoke 통과. Linux CI NOT VERIFIED ([roadmap P2](docs/planning/roadmap.md)) |
| CLI M1–M5 | **PARTIAL** | P1 항목 반영 완료, 실 API NOT VERIFIED |
| M6 audit + policy middleware | **COMPLETE** | Linux `./verify.sh --suite all`; 호출부 보정은 P1-2·P1-7 |
| git credential 호스트 매핑 (`[git."HOST"]`, `setup-git --host`) | **COMPLETE** | P1-1 반영, `c4b09ea` 커밋·mac-smoke·설치본 검증 통과 |
| `kc audit` / `kc policy check` | **COMPLETE** | 단위·integration |
| [0005-m6-audit-policy-middleware.md](docs/decisions/0005-m6-audit-policy-middleware.md) | **COMPLETE** | |
| mac-smoke (Darwin) | **COMPLETE** | 2026-10-25 실 Mac(GUI 로그인·keychain unlocked)에서 `KC="$PWD/dist/kc" bash scripts/mac-smoke.sh` 통과 |
| 샌드박스 종료 3 (실 Mac) | **COMPLETE** | `sandbox-exec` 안 `security` exit 44 + `... parameters ... not valid` → exit 3 매핑, 회귀 테스트 추가 |
| Cursor Cloud 실 push | **NOT VERIFIED** | |
| gh 실 push | **NOT VERIFIED** | |
| 에이전트 실사용 + 정책 운영 | **NOT VERIFIED** | |
| `exec` 환경 키체인 항목 노출 | **NOT VERIFIED** | mac-smoke는 `ls --json`만 확인 |
| 설치본 `~/.local/bin/kc` | **COMPLETE** | `v0.5.0-1-gc4b09ea`, 실 Mac mac-smoke 재통과 |
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

P0-1~P0-7, P1-1~P1-9, P2-1~P2-6 구현·커밋·설치본 검증은 끝났다. 남은 일은 Linux CI와 릴리스 게이트 검증이다.

1. Linux CI에서 `./verify.sh --suite all` 실행 (`.github/workflows/test.yml`가 push/PR에서 자동 실행). 통과 시 P0·P1·P2 상태를 COMPLETE로 갱신.
2. 실 Mac mac-smoke는 2026-10-25 통과했다(keychain 쓰기/읽기·rebuild 읽기·`ls --json`의 `kc.secret.*`·ai-core keychain 읽기·`kc.age` 왕복·link·setup-git·샌드박스 종료 3). 재실행 시에도 **SSH 세션에서는 불가**: `launchctl managername=Background` + 잠긴 login keychain 때문에 `security -i`가 `-25308 User interaction is not allowed`로 실패한다. Mac의 GUI 로그인 세션에서 keychain을 해제한 뒤 `KC="$PWD/dist/kc" bash scripts/mac-smoke.sh`로 실행한다.
3. P2-6: **COMPLETE** — `c4b09ea`(코드)·`39a19c3`(문서), 설치본 `~/.local/bin/kc` 재빌드·mac-smoke 통과. 버전 문자열 해시를 HEAD에 맞추려면 `bash scripts/lifecycle.sh build-dev --install` 한 번 더.
4. 릴리스 게이트: Cursor/gh 실 push 각 1회, 에이전트 실사용 1회, `exec` 환경 키체인 노출 확인.

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
