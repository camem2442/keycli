---
status: implemented
---
# 맥 → 클라우드 전송 계약

원칙: **필요한 토큰만, 대상별로.** 저장소 전체 + `KC_KEY` 동기화는 하지 않는다([결정 0001](../decisions/0001-design-choices.md)).

## 경로 우선순위

1. **플랫폼 시크릿 → env 백엔드** (기본). 클라우드에 kc 저장소·`KC_KEY` 불필요.
2. **`kc push <target>`**: 공식 API/CLI로 맥에서 직접 등록.
3. **`kc export --format dotenv`**: `.env` 붙여넣기만 지원하는 플랫폼용. 클립보드로만, 자동 삭제, 화면 출력 금지.
4. **age 암호화 전달** (`kc export --to` → `kc import`): 플랫폼 시크릿 기능이 없는 VM·컨테이너용.

## push 대상

| 대상 | 방식 | 기본값 | 비고 |
|---|---|---|---|
| `cursor` | Cursor Cloud Agents API `PUT /v1/environments/{id}/secrets/{name}` | 타입 `runtime_secret`, `--repos`로 범위 제한 | API 키: resolver 이름 `cursor-api-key`(기본 alias: `cursor_api_key`, git `[git."origin.cursor.com"]`와 동일) 또는 `KC_CURSOR_API_KEY`. 환경 ID: `--env-id` 또는 `KC_CURSOR_ENVIRONMENT_ID`. [공식 문서](https://cursor.com/docs/cloud-agent/api/endpoints) — Basic `API_KEY:` 인증, JSON body `value`/`type`/`repos`. `409 secret_name_ambiguous`, 값 1–4096바이트, 환경당 1000개. Linux httptest **PASS**; Cloud egress·401 계약 **PARTIAL** (KC-49); 성공 PUT·목록·DELETE Mac **COMPLETE** (KC-55, [증거](../planning/v07-operational-evidence.md)); `cursor_api_key` alias 경유는 dry-run만(KC-59). 팀 단위 엔드포인트: **NOT VERIFIED** |
| `gh` | `gh secret set NAME -f -` (dotenv 한 줄을 stdin) `[--user] [--repos …] --app codespaces\|actions\|agents` | `--app codespaces` | argv에 값 없음. fake `gh` 테스트 **PASS**; `--repos … --app actions` 실 호출 **PASS** (KC-49, Cloud); Mac `--repos camem2442/keycli --app actions` set/delete **COMPLETE** (KC-55, [증거](../planning/v07-operational-evidence.md)); `--user`+`codespaces`는 토큰 권한에 따라 **BLOCKED** 가능; Mac 다른 `--app`·`--user` **NOT VERIFIED** |

## export 형식

| 대상 플랫폼 | 형식 | 근거 |
|---|---|---|
| Codex Cloud, Claude Code 웹 | `--format dotenv` (클립보드) | 두 플랫폼 모두 `.env` 붙여넣기 지원 |
| Cursor | push 우선, 일괄 붙여넣기는 **NOT VERIFIED** | |

`--stdout`은 stdout이 TTY가 **아닐 때만** dotenv 출력. TTY에서는 클립보드만.

`--to-github USER`는 `https://github.com/USER.keys` 를 가져온다. **push 외 유일한 네트워크 사용** (export 시).

## age 전달

```bash
# 클라우드: 수신자 키 확보
kc init                       # 기존 identity(KC_KEY → receiver.key → kc.age)의 age1… 공개키만 stdout; 없으면 새로 만들어 receiver.key에 저장
kc init --print-identity      # identity(비밀값)를 stdout으로 — 스크립트용, 로그 금지
# 맥
kc export github openai --to age1...
kc export github --to-github <user>
# 클라우드
kc import < bundle.age
```

수신자 우선순위: `--to` → `--to-github`. SSH 수신자 사용 시 stderr 경고(암호문에 공개키 태그).

## 변경 추적

- 맥 로컬 원장 `~/.kc/ledger.json`: `{name, target, hmac, exported_at}`. HMAC 키는 암호화 파일 저장소의 `ledger_hmac_key` 필드. 원장 갱신과 store의 read-modify-write는 전용 `.lock` 파일 flock으로 프로세스 간 직렬화한다. `push`는 한 번의 `push`에서 여러 시크릿을 한 번의 load/save로 기록한다(`ledger.RecordAll`).
- `kc status`:보낸 뒤 값이 바뀐 항목. `kc ls --stale`은 `*` 및 `stale_export` (JSON)를 표시하며, 원장에 있는 이름만 읽는다.

## 단계별 가용성

| 환경 | 시크릿이 없는 단계 |
|---|---|
| Claude Code 웹 | 설정 스크립트 (환경변수 비어 있음) |
| Codex Cloud 구버전 | 에이전트 단계 (설정 단계에서만 존재) |
| Codespaces | 빌드 단계, dev container feature |

env에 값이 없으면 종료 코드 2와 함께 phase hint(설정·빌드 단계)를 stderr에 덧붙인다.

## 권장 운영

클라우드용 토큰은 맥용과 **따로 발급**하고 권한을 더 좁게. 한쪽 폐기가 다른 쪽에 영향 없게.
