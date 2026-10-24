---
status: implemented
---
# CLI 계약

## 에이전트용

| 명령 | 동작 |
|---|---|
| `kc ls [--json] [--all]` | 사용 가능한 이름과 메타데이터(저장소, 메모, 만료일). **값은 절대 출력하지 않음**. 정책으로 읽기 거부된 이름은 기본 숨김(`--all`이면 `(policy)` 표시·JSON `policy_denied`) |
| `kc get NAME [--source]` | 값만 stdout. `--source`는 **stderr**에 출처만 출력(예: `keychain:local.study.ai.groq/groq_api_key`, `env:GROQ_API_KEYS`). 값은 절대 포함하지 않음 |
| `kc exec [--only a,b] -- CMD...` | 매핑된 환경변수로 주입해 실행. 자식 종료 코드를 그대로 반환 |

이름 해석: 별칭 표(`github` = `gh` = `GITHUB_TOKEN` = `GH_TOKEN`)와 대소문자 무시. 없으면 stderr에 한 줄 `kc: NAME 없음. 사용자에게 'kc set NAME' 실행을 요청하세요.`

## 사람용

| 명령 | 동작 |
|---|---|
| `kc set NAME [--clip] [--note TEXT] [--expires DATE]` | 숨김 입력 또는 클립보드에서 저장. `--clip`은 저장 후 클립보드 비움 |
| `kc cp [NAME]` | 클립보드 복사, 30초 뒤 삭제(그새 다른 걸 복사했으면 유지). 이름 없으면 퍼지 선택 |
| `kc rm NAME` | 삭제 |
| `kc link NAME keychain:SERVICE[/ACCOUNT]` | 기존 키체인 항목을 config에 **참조만** 저장. 읽기는 `security` 경로(최초 1회 Always Allow 가능) |
| `kc unlink NAME` | config에서 link 제거 |
| `kc setup-git [--dry-run] [--undo]` | `git config --global credential.https://github.com.helper '!…/kc git-credential'`. 토큰은 gitconfig에 쓰지 않음. `gh`는 `kc exec --only github -- gh` 래퍼 스니펫 출력 |
| `kc git-credential get\|store\|erase` | git credential helper (`get`만 github.com 토큰 조회; store/erase는 no-op) |
| `kc rules [--target cursor\|claude\|codex\|agents] [--write PATH]` | 에이전트용 규칙 블록 출력·삽입 (`<!-- kc-rules-begin/end -->`) |
| `kc doctor [--json]` | 백엔드·config·KC_KEY/kc.age·PATH `kc` 충돌·키체인·클립보드·git helper (값 미출력) |
| `kc push cursor\|gh [NAME...] [--repos …]` | 플랫폼 시크릿에 직접 등록 ([전송 계약](transfer.md)) |
| `kc export NAME... --to KEY\|--to-github USER\|--format dotenv` | 암호화 번들 또는 클립보드용 dotenv |
| `kc import` | stdin의 age 번들을 저장소에 추가 |
| `kc status` | 내보낸 뒤 바뀐 항목 표시 |
| `kc doctor [--json]` | 백엔드·config·키·PATH 충돌 (값 미출력) |
| `kc plugin list [--json]` | `kc-backend-*` 발견, enabled, protocol v1, capabilities (enabled만 handshake) |
| `kc audit [--since DUR] [--name N] [--agent A] [--json]` | `~/.kc/audit.log` JSONL 조회(값·해시·길이 없음) |
| `kc policy check [--agent A] NAME` | 현재(또는 지정) 에이전트가 NAME 읽기 허용 여부(거부 시 종료 5) |

## 출력 형식

`ls --json`: `{"v":1,"items":[{"name":"","aliases":[],"backend":"","source":"","note":"","expires":null,"stale_export":false,"policy_denied":false}]}`. `source`는 link·출처 표시용(선택). `stale_export`: 원장 기록 이후 값 변경. `policy_denied`: 에이전트 정책으로 `get` 등이 거부될 이름. 필드 추가는 호환, 삭제·의미 변경은 `v` 증가.

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

정책 섹션이 없으면 모든 읽기 허용. 에이전트별 `[policy.agent.NAME]`만 deny-by-default(글로브 허용). `default`·`human`은 미식별·TTY·CI 등에 적용.

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
