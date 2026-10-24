---
status: implemented
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
- 외부 저장소는 `kc-backend-<name>` 플러그인 + config `type = "external"`만 사용 ([plugin-protocol.md](../contracts/plugin-protocol.md)). `kc plugin list --json`으로 enabled·capabilities 확인.

## 클라우드 에이전트 환경 (Cursor / GitHub)

맥에 토큰이 있고 클라우드 VM에는 없을 때 ([transfer.md](../contracts/transfer.md)):

### 설치

```bash
curl -fsSL https://raw.githubusercontent.com/camem2442/keycli/main/scripts/install.sh | bash
```

(또는 릴리스 `kc-linux-*` 바이너리)

### Cursor Cloud Agents

1. 맥: Cursor API 키를 `kc set cursor-api-key --clip` (또는 키체인).
2. 맥: `kc push cursor --env-id <ENV_ID> OPENAI_API_KEY GITHUB_TOKEN` (`--repos`로 범위 제한 가능, `--dry-run`으로 확인).
3. 클라우드: 플랫폼 시크릿이 env로 주입되면 `kc get` / `kc exec`만으로 충분 (네트워크 불필요).

### GitHub Codespaces / Actions

```bash
kc push gh --user MY_TOKEN          # 사용자 시크릿
kc push gh --repos org/repo TOKEN --app codespaces
```

`gh` 자체는 `kc exec --only github -- gh …` ([setup-git](#github-git--gh)).

### age 번들 (시크릿 API 없는 VM)

```bash
# VM
kc init    # age1… 를 맥에 전달
# 맥
kc export github --to age1… > bundle.age
# VM
kc import < bundle.age
```

### dotenv 붙여넣기 (Codex / Claude Code 웹)

```bash
kc export --format dotenv A B   # 클립보드만 (TTY에 출력 안 함)
```

### 변경 추적

`kc status` / `kc ls`의 `*` — 맥에서 값을 바꾼 뒤 재 `push` 필요.
