---
status: implemented
---
# CLI 계약

## 에이전트용

| 명령 | 동작 |
|---|---|
| `kc ls [--json] [--all] [--stale]` | 사용 가능한 이름과 메타데이터(저장소, 메모, 만료일). **값은 절대 출력하지 않음**. 정책으로 읽기 거부된 이름은 기본 숨김(`--all`이면 `(policy)` 표시·JSON `policy_denied`). `--stale`은 원장에 기록된 이름만 읽어 `*`/`stale_export`를 표시하며 거부 이름은 읽지 않음 |
| `kc get NAME [--source]` | 값만 stdout. `--source`는 **stderr**에 출처만 출력(예: `keychain:local.study.ai.groq/groq_api_key`, `env:GROQ_API_KEYS`). 값은 절대 포함하지 않음 |
| `kc exec [--only a,b] [--require] -- CMD...` | 매핑된 환경변수로 주입해 실행. `--require`는 `--only`에 적은 이름이 **모두** resolve·policy 통과할 때만 자식 시작(누락·거부 시 exit 2/5, 자식 0회). `--only` 생략 시 조회 가능한 전체 시크릿 주입(에이전트는 `--require --only` 권장). 자식 종료 코드를 그대로 반환. 자식 환경에서 `KC_KEY`·`KC_AGENT`는 제거 |
| `kc mcp` | MCP (Model Context Protocol) JSON-RPC 2.0 stdio 서버 실행 (`list_secrets`, `get_secret`, `policy_check`, `doctor` 네이티브 도구 제공) |

이름 해석: 별칭 표(`github` = `gh` = `GITHUB_TOKEN` = `GH_TOKEN`)와 대소문자 무시. 없으면 stderr에 한 줄 `kc: NAME 없음. 사용자에게 'kc set NAME' 실행을 요청하세요.`

## 사람용

| 명령 | 동작 |
|---|---|
| `kc [interactive]` (별칭 `kc i`, `kc ui`) | 대화형 TUI 시크릿 관리자(목록 검색, 상세 조회, 복사, 생성, 삭제) |
| `kc set [NAME] [--clip] [--note TEXT] [--expires DATE]` | 숨김 입력 또는 클립보드에서 저장. 이름 생략 시 대화형 마법사 실행. 파이프 입력의 끝 개행 1개는 제거. `--clip`은 저장에 성공한 뒤에만 클립보드 비움 |
| `kc cp [NAME]` | 클립보드 복사, 30초 뒤 삭제(그새 다른 걸 복사했으면 유지). 이름 생략 시 실시간 대화형 검색 및 선택 |
| `kc get [NAME]` | 값만 stdout. 이름 생략 시 대화형 선택 |
| `kc rm [NAME]` | 삭제. 이름 생략 시 대화형 선택 및 확인 |
| `kc init` (별칭 `keygen`) | `KC_KEY` → `~/.kc/receiver.key` → 키체인 `kc.age` 순으로 **기존 identity**를 찾아 공개 recipient(`age1…`)만 stdout에 출력. 없을 때만 새 identity를 `~/.kc/receiver.key`(0600)에 저장. `--print-identity`는 identity(비밀값)를 stdout에 출력 |
| `kc link NAME keychain:SERVICE[/ACCOUNT]` | 기존 키체인 항목을 config에 **참조만** 저장. 읽기는 `security` 경로(최초 1회 Always Allow 가능) |
| `kc unlink NAME` | config에서 link 제거 |
| `kc setup-git [--host HOST] [--dry-run] [--undo]` | `git config --global credential.https://HOST.helper '!…/kc git-credential'` (기본 `github.com`). 토큰은 gitconfig에 쓰지 않음. 대상 호스트의 `[git."HOST"]` 매핑이 없으면 명시적으로 기록. 기본 호스트면 `gh`용 `kc exec --only github -- gh` 래퍼 스니펫 출력 |
| `kc git-credential get\|store\|erase` | git credential helper. `get`은 config `[git."HOST"]` 매핑을 조회해 username/password 출력, 매핑 없는 호스트는 무응답. `[git."github.com"]`이 없을 때만 내장 기본값을 쓰되 시크릿이 없으면 무응답. 명시 매핑의 시크릿이 없으면 종료 2. `command`가 있으면 secret을 env로 주입해 실행한 stdout을 password로 사용(네트워크는 그 명령에서만). store/erase는 no-op |
| `kc rules [--target cursor\|claude\|codex\|agents] [--write PATH]` | 에이전트용 규칙 블록 출력·삽입 (`<!-- kc-rules-begin/end -->`) |
| `kc doctor [--json]` | 백엔드·config·identity 출처(`KC_KEY`/`receiver.key`/`kc.age`)·PATH `kc` 충돌·키체인·클립보드·git helper (값 미출력). JSON `kc_age`는 `env`/`file`/`keychain`/`missing`/`locked`/`blocked`, `identity_path`는 file일 때만 |
| `kc push cursor\|gh [NAME...] [--repos …]` | 플랫폼 시크릿에 직접 등록 ([전송 계약](transfer.md)) |
| `kc export NAME... --to KEY\|--to-github USER\|--format dotenv` | 암호화 번들 또는 클립보드용 dotenv |
| `kc import` | stdin의 age 번들을 저장소에 추가 |
| `kc status` | 내보낸 뒤 바뀐 항목 표시. 읽을 수 없는 이름은 값 없이 stderr에 이름·사유를 보고 |
| `kc doctor [--json]` | 백엔드·config·키·PATH 충돌 (값 미출력) |
| `kc plugin list [--json]` | `kc-backend-*` 발견, enabled, protocol v1, capabilities (enabled만 handshake) |
| `kc audit [--since DUR] [--name N] [--agent A] [--json]` | `~/.kc/audit.log` JSONL 조회(값·해시·길이 없음). `--json`은 항목이 없어도 `[]` |
| `kc policy check [--agent A] NAME` | 현재(또는 지정) 에이전트가 NAME 읽기 허용 여부(거부 시 종료 5) |
| `kc wire opencode-vertex [--dry-run] [--config PATH]` | `vertex-byok` 로컬 프록시용 OpenCode `provider` 블록을 `opencode.json`에 병합. `apiKey`는 `{env:PROXY_API_KEY}` |

## 출력 형식

`ls --json`: `{"v":1,"items":[{"name":"","aliases":[],"backend":"","source":"","note":"","expires":null,"stale_export":false,"policy_denied":false}]}`. `source`는 link·출처 표시용(선택). `stale_export`: 원장 기록 이후 값 변경(`--stale`일 때 계산). `policy_denied`: 에이전트 정책으로 `get` 등이 거부될 이름. 필드 추가는 호환, 삭제·의미 변경은 `v` 증가.

## 미들웨어 설정

`middleware = ["audit", "policy"]` 순서는 바깥→안쪽(첫 항목이 가장 바깥). 미설정 시 미들웨어 없음(기존 동작).

```toml
middleware = ["audit", "policy"]

[audit]
enabled = true          # false면 감사 미들웨어 no-op
required = false        # true면 로그 쓰기 실패 시 성공한 읽기도 실패
path = "~/.kc/audit.log"
max_bytes = 5242880

[policy.agent.codex]
allow = ["github", "groq*"]

[policy.agent.default]
allow = ["demo"]
```

정책 섹션이 없으면 모든 읽기 허용. 에이전트별 `[policy.agent.NAME]`만 deny-by-default(글로브 허용). `default`·`human`은 미식별·TTY·CI 등에 적용. **`[policy.agent.*]` 규칙만으로는 강제되지 않는다.** `middleware`에 `"policy"`가 있을 때만 `get` 거부와 `ls`의 `(policy)`/`policy_denied` 표시가 함께 적용된다. `kc policy check`도 같은 기준을 쓰며, 규칙만 있고 미들웨어가 없으면 "not enforced"를 알린다.

## git credential 호스트 매핑

`kc git-credential get`은 stdin의 `host=`로 config의 `[git."HOST"]`를 찾는다. 매핑이 없으면 아무것도 출력하지 않는다(no-op).

- **명시 매핑만 강제된다.** config에 쓴 `[git."HOST"]`의 시크릿이 없으면 종료 코드 2로 실패한다.
- `[git."github.com"]`이 없으면 내장 기본값(secret `github`, username `git`)을 쓰지만, 이 암묵적 기본값은 시크릿이 없을 때 조용히 무응답한다(git의 다른 helper로 넘어갈 수 있게).
- `kc setup-git`은 대상 호스트의 매핑이 없으면 config에 명시적으로 기록한다. 기본 호스트는 secret `github`, 그 외 호스트는 secret 이름을 호스트로 둔다.

```toml
[git."github.com"]
secret = "github"        # 생략 시 호스트 이름을 secret 이름으로 사용
username = "git"         # 생략 시 "git"

[git."origin.cursor.com"]
secret = "cursor_api_key"
username = "x-access-token"
command = "origin auth token"   # secret을 env로 주입해 실행, stdout(trim)이 password
```

- `secret`은 일반 resolver 경로로 읽히므로 **audit·policy 미들웨어가 그대로 적용**된다.
- `command`는 `sh -c`로 실행되며, secret은 `resolve.EnvNames(secret)`이 만든 환경변수로만 전달된다(argv 금지). stdout만 password로 쓰고 stderr는 전달하지 않는다. 60초 타임아웃.
- **kc 본체는 이 경로에서 네트워크를 쓰지 않는다.** 네트워크는 사용자가 config에 명시한 `command`에서만 발생한다.
- `command`가 실패하거나 빈 출력이면 종료 코드 1과 stderr 메시지(값 미포함)로 실패한다.

## 종료 코드

| 코드 | 의미 |
|---|---|
| 0 | 성공 |
| 1 | 일반 오류 |
| 2 | 이름 없음 |
| 3 | 샌드박스 등 환경 제약으로 차단 |
| 4 | 저장소 잠김 / 키(`KC_KEY`) 없음 |
| 5 | 정책 거부 — stderr에 사용자에게 허용 목록 변경 요청 안내 |
| 64 | 사용법 오류 |
