---
status: proposed
---
# 코딩 에이전트 설정 (Cursor / Claude Code / Codex / Antigravity)

목표: 토큰이 없을 때 에이전트가 **사람에게 승인·복사를 요청하지 않고** `kc`로 직접 조회한다.

## 공통 규칙

1. `kc ls --json`으로 이름만 확인한다 (값 금지).
2. 명령 실행은 `kc exec --only NAME -- COMMAND…`를 우선한다. 단순 조회만 필요하면 `kc get NAME` (stdout만, 로그·커밋 금지).
3. 종료 코드 **3**은 샌드박스/키체인 차단 — 사용자에게 샌드박스 밖 실행 또는 env 시크릿을 요청한다.
4. ai-core 키체인 항목(`local.study.ai.*`)은 **읽기 전용**이다.

규칙 블록 자동 삽입:

```bash
kc rules --target agents --write AGENTS.md
kc rules --target cursor --write .cursor/rules/keycli.mdc
kc rules --target claude --write CLAUDE.md
```

## Cursor

- 프로젝트 규칙: [`.cursor/rules`](https://docs.cursor.com/context/rules) 또는 루트 `AGENTS.md`.
- `kc rules --target cursor --write .cursor/rules/keycli.mdc` 권장.

## Claude Code

- 프로젝트 루트 [`CLAUDE.md`](https://docs.anthropic.com/en/docs/claude-code) 또는 `.claude/CLAUDE.md`.
- `kc rules --target claude --write CLAUDE.md`

## Codex / 기타 CLI 에이전트

- 루트 `AGENTS.md`에 동일 블록 (`kc rules --target codex --write AGENTS.md`).

## GitHub (`git` / `gh`)

```bash
kc setup-git              # git credential helper (토큰은 gitconfig에 저장하지 않음)
# gh는 셸 함수 스니펫을 출력함 — kc exec로 주입:
kc exec --only github -- gh pr list
```

`github` 이름은 config 별칭으로 `GH_TOKEN`·`GITHUB_TOKEN`·`gh`에 매핑된다 ([cli.md](../contracts/cli.md)).

## macOS 키체인

- 다른 앱이 만든 항목: `kc link NAME keychain:SERVICE/ACCOUNT`
- ai-core provider(`groq`, `openai`, …)는 자동 어댑터 ([0003](../decisions/0003-aicore-keychain.md)).
- mac-smoke·실기 검증 시 셸의 `GROQ_API_KEYS` 등이 키체인을 가리지 않도록 `env -i`로 최소 환경을 쓴다.

## Linux / CI

- `KC_KEY` 또는 file store ([backends.md](../contracts/backends.md)).
- `kc doctor --json`으로 백엔드 상태 확인.
