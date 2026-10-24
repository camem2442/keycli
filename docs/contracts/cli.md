---
status: proposed
---
# CLI 계약

## 에이전트용

| 명령 | 동작 |
|---|---|
| `kc ls [--json]` | 사용 가능한 이름과 메타데이터(저장소, 메모, 만료일). **값은 절대 출력하지 않음** |
| `kc get NAME` | 값만 stdout에 출력(개행 없음 옵션 `-n`). 대화형 질문·승인 창 없음 |
| `kc exec [--only a,b] -- CMD...` | 매핑된 환경변수로 주입해 실행. 자식 종료 코드를 그대로 반환 |

이름 해석: 별칭 표(`github` = `gh` = `GITHUB_TOKEN` = `GH_TOKEN`)와 대소문자 무시. 없으면 stderr에 한 줄 `kc: NAME 없음. 사용자에게 'kc set NAME' 실행을 요청하세요.`

## 사람용

| 명령 | 동작 |
|---|---|
| `kc set NAME [--clip] [--note TEXT] [--expires DATE]` | 숨김 입력 또는 클립보드에서 저장. `--clip`은 저장 후 클립보드 비움 |
| `kc cp [NAME]` | 클립보드 복사, 30초 뒤 삭제(그새 다른 걸 복사했으면 유지). 이름 없으면 퍼지 선택 |
| `kc rm NAME` | 삭제 |
| `kc link NAME keychain:SERVICE[/ACCOUNT]` | 기존 키체인 항목 별칭 |
| `kc setup-git` | gh/git credential 연결 |
| `kc rules` | AGENTS.md/CLAUDE.md용 규칙 문장 출력 |
| `kc push cursor\|gh [NAME...] [--repos …]` | 플랫폼 시크릿에 직접 등록 ([전송 계약](transfer.md)) |
| `kc export NAME... --to KEY\|--to-github USER\|--format dotenv` | 암호화 번들 또는 클립보드용 dotenv |
| `kc import` | stdin의 age 번들을 저장소에 추가 |
| `kc status` | 내보낸 뒤 바뀐 항목 표시 |
| `kc backends`, `kc doctor`, `kc plugin list` | 진단 |

## 출력 형식

`ls --json`: `{"v":1,"items":[{"name":"","aliases":[],"backend":"","note":"","expires":null}]}`. 필드 추가는 호환, 삭제·의미 변경은 `v` 증가.

## 종료 코드

| 코드 | 의미 |
|---|---|
| 0 | 성공 |
| 1 | 일반 오류 |
| 2 | 이름 없음 |
| 3 | 샌드박스 등 환경 제약으로 차단 |
| 4 | 저장소 잠김 / 키(`KC_KEY`) 없음 |
| 5 | 정책 거부(미들웨어 도입 후) |
| 64 | 사용법 오류 |
