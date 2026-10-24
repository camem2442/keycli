---
status: proposed
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
| `cursor` | Cursor Cloud Agents API `PUT /v1/environments/{id}/secrets/{name}` | 타입 `runtime_secret`, `--repos`로 범위 제한 | API 키는 맥 키체인에 보관. `409 secret_name_ambiguous` 처리. 값 1–4096바이트, 환경당 1000개. Build Secret은 API 불가. 팀 단위 엔드포인트는 **NOT VERIFIED** |
| `gh` | `gh secret set -f - --user [--repos …] --app codespaces|actions|agents` | stdin 전달 | argv에 값 없음. Codespaces는 빌드 단계에서 시크릿 없음, 변경은 재시작 후 반영 |

## export 형식

| 대상 플랫폼 | 형식 | 근거 |
|---|---|---|
| Codex Cloud, Claude Code 웹 | `--format dotenv` (클립보드) | 두 플랫폼 모두 `.env` 붙여넣기 지원 |
| Cursor | push 우선, 일괄 붙여넣기는 **NOT VERIFIED** | |

Codex·Claude Code의 **network secret**은 VM에 값이 들어오지 않아 kc가 읽을 수 없다. 프록시 주입으로 충분하면 그쪽을 권장하고, 원값이 필요한 경우에만 환경변수로 등록한다.

## age 전달

```bash
# 클라우드: 수신자 키 확보
kc init                       # age1… 공개키 출력 (SSH 키가 있으면 생략 가능)
# 맥
kc export github openai --to age1...          # 또는 --to "ssh-ed25519 …"
kc export github --to-github <user>           # github.com/<user>.keys
# 클라우드
kc import < bundle.age
```

수신자 우선순위: `--to` → `--to-github` → `kc init` 키. SSH 수신자는 암호문에 공개키 태그가 남는다는 경고를 표시한다. ssh-agent·하드웨어 키는 age 복호화에 사용 불가.

## 변경 추적

- 맥 로컬 원장 `~/.kc/ledger.json`: `{name, target, hmac(value), exported_at}`. 값·평문 해시는 저장하지 않는다(HMAC 키는 kc 저장소 안).
- `kc status`: 내보낸 뒤 값이 바뀐 항목을 "재전송 필요"로 표시. `kc ls`에도 표시.

## 단계별 가용성

| 환경 | 시크릿이 없는 단계 |
|---|---|
| Claude Code 웹 | 설정 스크립트 (환경변수 비어 있음) |
| Codex Cloud 구버전 | 에이전트 단계 (설정 단계에서만 존재) |
| Codespaces | 빌드 단계, dev container feature |

env에 값이 없으면 종료 코드 2와 함께 "설정·빌드 단계라 아직 주입되지 않았을 수 있음"을 덧붙인다.

## 권장 운영

클라우드용 토큰은 맥용과 **따로 발급**하고 권한을 더 좁게. 한쪽 폐기가 다른 쪽에 영향 없게.
