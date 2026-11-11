---
status: implemented
---
# CLI 계약

## 에이전트용

| 명령 | 동작 |
|---|---|
| `kc ls [--json] [--all] [--stale]` | 사용 가능한 이름과 메타데이터(저장소, 메모, 만료일). **값은 절대 출력하지 않음**. 정책으로 읽기 거부된 이름은 기본 숨김(`--all`이면 `(policy:<reason>)` 표시·JSON `policy_denied`). JSON은 추가로 `policy_reason`(`deny_hit`/`not_allowed`/`invalid_pattern`)과 `policy_rule`(매칭된 패턴)을 **선택 필드**로 싣는다 — 스키마 v1의 하위 호환 추가다. `--stale`은 원장에 기록된 이름만 읽어 `*`/`stale_export`를 표시하며 거부 이름은 읽지 않음 |
| `kc get NAME [--source]` | 값만 stdout. `--source`는 **stderr**에 출처만 출력(예: `keychain:local.study.ai.groq/groq_api_key`, `env:GROQ_API_KEYS`). 값은 절대 포함하지 않음 |
| `kc exec [--only a,b] [--require] -- CMD...` | 매핑된 환경변수로 주입해 실행. `--require`는 `--only`에 적은 이름이 **모두** resolve·policy 통과할 때만 자식 시작(누락·거부 시 exit 2/5, 자식 0회). `--only` 생략 시 조회 가능한 전체 시크릿 주입(에이전트는 `--require --only` 권장). 자식 종료 코드를 그대로 반환. 자식 환경에서 `KC_KEY`·`KC_AGENT`는 제거 |
| `kc policy show [--agent A] [--json]` | 해당 에이전트에 실제 적용되는 `allow`/`deny`와 출처(`explicit`/`default`/`none`), 강제 여부(`enforced`)를 보여준다. `--json`은 `{"v":1,...}`. 규칙이 깨져 있어도 동작한다; 정상 규칙에서는 유효 정책·alias 테이블의 SHA-256 `policy_digest`를 추가하며, 패턴 오류 시 해당 필드를 생략한다. [P5 계약](effective-policy-digest-v1.md) |
| `kc policy validate [--json]` | 모든 `allow`/`deny` 패턴을 검증해 **첫 오류가 아니라 전부** 보고한다. 정상이면 exit 0, 오류가 있으면 각 줄을 stderr에 쓰고 exit 1. `--json`은 `{"v":1,"ok":bool,"agents":n,"errors":[...]}` |
| `kc mcp [--allow-secret-values]` | MCP JSON-RPC 2.0 stdio 서버. 기본 **메타데이터 전용**(`list_secrets`, `policy_check`, `doctor`); `get_secret`은 서버 소유자가 `--allow-secret-values`로 명시적으로 허용한 경우에만 노출·실행 |

**릴리스/소스 구분:** 이 MCP 계약은 게시된 **v0.7.2 바이너리**와 PR #16 병합 이후 소스에서 적용됩니다. **v0.7.1 이하**는 `get_secret` 기본 노출·opt-in 옵션 부재가 있으므로 동일한 기본 보안 계약으로 간주하지 않습니다. [릴리스 증거](../planning/v072-release-evidence.md)는 Linux 배포 자산의 SHA256 및 재설치 테스트에 한정됩니다.

이름 해석: 별칭 표(`github` = `gh` = `GITHUB_TOKEN` = `GH_TOKEN`)와 대소문자 무시. 없으면 stderr에 한 줄 `kc: NAME 없음. 사용자에게 'kc set NAME' 실행을 요청하세요.`

## 사람용

| 명령 | 동작 |
|---|---|
| `kc [interactive]` (별칭 `kc i`, `kc ui`) | 대화형 TUI 시크릿 관리자(목록 검색, 상세 조회, 복사, 생성, 삭제) |
| `kc set [NAME] [--clip] [--note TEXT] [--expires DATE] [--normalize] [--no-verify] [--force]` | 숨김 입력 또는 클립보드에서 저장. 이름 생략 시 대화형 마법사 실행. 기본은 시크릿 **바이트 보존**; 파이프 stdin은 끝 개행 1개만 제거, `--clip`은 텍스트 끝 `\n`만 제거. 붙여넣기 PAT 정리는 `--normalize`. `--clip`은 저장에 성공한 뒤에만 클립보드 비움. Cloudflare 등 provider 이름이면 저장 전 [set-time verify](set-time-verify-v1.md)(기본 on; `--no-verify` 생략, 실패 시 중단·`--force`로 무시) |
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
| `kc delivery plan --profile NAME [--config PATH] [--json]` | GitHub Actions WIF·Cloudflare 비활성 버전·OCI WIF의 오프라인 계획. [설정과 출력](../guides/delivery-plan.md) |
| `kc auth probe --profile NAME --offline --claims PATH [--config PATH] [--json]` | GitHub Actions WIF의 합성 claim을 로컬에서 비교. [입력 형식](../guides/delivery-plan.md#github-actions-wif-합성-claim-검사) |
| `kc push cursor\|gh\|cloudflare [NAME...] [--repos …] [--worker W --worker-env E --wrangler-config PATH]` | 플랫폼 시크릿에 직접 등록 ([전송 계약](transfer.md)) |
| `kc export NAME... --to KEY\|--to-github USER\|--format dotenv` | 암호화 번들 또는 클립보드용 dotenv |
| `kc import [--force]` | stdin의 age 번들을 저장소에 추가. 번들은 만료를 싣지 않으며, **기존 이름을 덮어쓰려 하면 거부**한다(`--force` 필요). 성공 시 "no expiry" 경고를 stderr에 남긴다 |
| `kc status` | 내보낸 뒤 바뀐 항목 표시. 읽을 수 없는 이름은 값 없이 stderr에 이름·사유를 보고 |
| `kc doctor [--json]` | 백엔드·config·키·PATH 충돌 (값 미출력) |
| `kc plugin list [--json]` | `kc-backend-*` 발견, enabled, protocol v1, capabilities (enabled만 handshake) |
| `kc audit [--since DUR] [--name N] [--agent A] [--json]` | `~/.kc/audit.log` JSONL 조회(값·해시·길이 없음). `--json`은 항목이 없어도 `[]`. 정책 거부 레코드는 `outcome=denied`에 `reason`·`rule`을 선택 필드로 함께 남긴다 |
| `kc policy check [--agent A] NAME` | 현재(또는 지정) 에이전트가 NAME 읽기 허용 여부(거부 시 종료 5) |
| `kc wire opencode-vertex [--dry-run] [--config PATH]` | `vertex-byok` 로컬 프록시용 OpenCode `provider` 블록을 `opencode.json`에 병합. `apiKey`는 `{env:PROXY_API_KEY}` |
| `kc wire cursor-vertex [--dry-run] [--db PATH] [--base-url URL] [--restart]` | `vertex-byok` Quick Tunnel URL을 Cursor 데스크톱의 `state.vscdb`(`openAIBaseUrl`, `useOpenAIKey=true`)에 자동 반영. 실행 중인 Cursor의 DB를 `--restart` 없이 수정하지 않음(실패 처리); `--dry-run`은 항상 무변경. `--restart` 시 정상 종료 후 DB 기록 및 재실행 |

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

[policy.agent.cursor]
allow = ["*"]
deny  = ["github", "sogang-*"]

[policy.agent.default]
allow = ["demo"]
```

정책 섹션이 없으면 모든 읽기 허용. 에이전트별 `[policy.agent.NAME]`만 deny-by-default(글로브 허용). `default`·`human`은 미식별·TTY·CI 등에 적용. **`deny`는 `allow`보다 먼저 평가된다** — 넓은 `allow` 아래 좁은 예외를 둘 때 쓴다(`allow = ["*"]` + `deny = ["github"]`). `deny`만 있고 `allow`가 비어 있으면 여전히 deny-by-default이며, `deny`는 allow를 만들어내지 않는다. **잘못된 글로브는 조용히 무시되지 않는다**: 설정 로드 자체는 성공하지만 `doctor`·`policy`·`rules`를 제외한 모든 명령이 에이전트·목록·패턴을 지목해 실패(exit 1)하고, 정책 미들웨어도 fail-closed로 전부 거부한다. 진단 명령은 계속 동작해 원인을 확인할 수 있다. **`[policy.agent.*]` 규칙만으로는 강제되지 않는다.** `middleware`에 `"policy"`가 있을 때만 `get` 거부와 `ls`의 `(policy)`/`policy_denied` 표시가 함께 적용된다. `kc policy check`도 같은 기준을 쓰며, 규칙만 있고 미들웨어가 없으면 "not enforced"를 알린다.

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

## Rotation

`kc rotation`은 JSON v1 메타데이터를 출력한다. 아래 파일 경로에는 절대 경로를 사용한다.

| 명령 | 입력 |
| --- | --- |
| `kc rotation plan --input <metadata.json>` | 로컬 세대와 필수 소비자 메타데이터 |
| `kc rotation status --journal <directory> --plan <digest>` | 로컬 journal과 PR-07 회전 세션 |
| `kc rotation status --profile <writer.json> <operation-id>` | 등록 작업의 인증된 Gateway 상태 |
| `kc rotation submit --profile <writer.json> <operation-id>` | 등록 작업의 인증된 제출 |
| `kc rotation reconcile --profile <writer.json> <operation-id>` | 원본 작업의 인증된 독립 관측 요청 |

계획 입력은 현재 UID 소유의 단일 링크 일반 파일이며 모드는 `0600` 또는
`0400`이다. JSON의 최상위 필드는 `v`, `old`, `new`, `required_consumers`다.
`v`는 1이며 각 세대의 필드는 `alias`, `kind`, `provider`, `target`,
`number`다. `required_consumers`는 고유한 소비자 ID 배열이다. 로컬 조회는
비밀 저장소를 열지 않는다. 저장된 소비자 증거와 grace 시각은 독립 검증이
연결되기 전까지 미검증으로 표시한다.

writer 프로필은 현재 UID가 소유하는 `0600` 파일이다. 최상위 필드는 `v`,
`gateway_config`, `session_key_file`, `goal_id`, `owner`, `session`,
`generation`, `operations`다. `gateway_config`는 별도로 보호된 Gateway
HTTPS 주소 설정 파일이며 `session_key_file`은 현재 세션 키 파일이다. 각
`operations` 항목의 정확한 필드는 다음과 같다.

- `operation`과 `mutation_id`
- `provider`와 `provider_operation`
- `resource_digest`, `target_digest`, `plan_digest`, `grant_digest`

명령의 `operation-id`는 프로필의 `operation`에 해당한다. Gateway가 호스트
등록과 현재 세션을 다시 검증한다. 제출 응답을 확인할 수 없으면 원래
`mutation_id`로 상태를 한 번 조회한다. [Gateway 설정 계약](effect-gateway-v1.md).

출력에는 `v`, `command`와 해당되는 `plan`, `rotation`, `effect`,
`error`가 있다. Gateway 결과에는 등록 ID인 `operation`도 표시한다.
`effect.outcome`은 `ACKNOWLEDGED_UNVERIFIED`, `UNKNOWN_EFFECT`,
`OBSERVED_APPLIED`, `OBSERVED_NO_EFFECT`, `DUPLICATE`,
`SUBMIT_UNCONFIRMED`를 구분한다. `journal_state`는 Gateway가 확인한
경우에만 표시한다. `status_lookup`은 제출 이후 상태 조회를 시도했는지
나타낸다. 조회로 대체된 응답에는 원래 제출의 `provider_calls`를 표시하지
않는다. 오류는 `error.code`로 표시한다.

| 종료 코드 | Rotation 명령의 의미 |
| --- | --- |
| `0` | 계획·상태 조회 성공, 적용 관측 확인 또는 복구에서 효과 없음 확인 |
| `2` | 인수, 계획 메타데이터 또는 등록 입력 오류 |
| `3` | 사용 불가, 응답 미확인, ACK만 수신 또는 아직 미결인 제출·복구 |
| `5` | 인증·권한 거부 |

성공적으로 조회한 상태에는 `UNKNOWN_EFFECT`가 포함될 수 있다. 이때 상태
조회 자체는 `0`을 반환한다.
