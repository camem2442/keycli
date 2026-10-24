---
status: active
---
# 로드맵

| 단계 | 내용 | 종료 조건 |
|---|---|---|
| M1 | cobra 골격, config, resolver, env·file 백엔드, `ls/get/exec/set/rm`, 종료 코드 | Linux에서 `verify.sh --suite unit` |
| M2 | keychain 백엔드(`security`), `link`, ai-core 읽기 어댑터 | macOS 수동 체크리스트 + 가짜 구현 테스트 |
| M3 | 클립보드 `cp`/`set --clip`, OSC 52, 자동 삭제 | 맥·SSH 수동 확인 |
| M4 | `setup-git`, `rules`, `doctor`, 에이전트별 설정 가이드 | Cursor/Claude Code/Codex/Antigravity 각 1회 실사용 |
| M4.5 | `push cursor`, `push gh`, `export --format dotenv`, age `export/import`, 원장·`status` | **COMPLETE** (Linux `verify.sh --suite all`; 실 Cursor/gh는 NOT VERIFIED) |
| M5 | 외부 하위 명령·백엔드 프로토콜 | **COMPLETE** (`kc plugin list`, 예제 백엔드, Linux `verify.sh --suite all`) |
| 이후 | 미들웨어(감사·허용·가림), 1Password 백엔드 | DEFERRED |

## 진행 (2026-10-24)

| 항목 | 상태 |
|---|---|
| M1 | **COMPLETE** (v0.1.0) |
| M2 keychain·ai-core | **COMPLETE** (Linux fake + mac-smoke) |
| M2 `link` / `unlink` | **COMPLETE** |
| M3 clip/cp | **COMPLETE** (M1에 포함) |
| M4 `setup-git`, `rules`, `doctor`, [agents.md](../guides/agents.md) | **COMPLETE** (Linux CI); 에이전트 실사용은 **NOT VERIFIED** |
| M4.5 `push`/`export`/`import`/`status` | **COMPLETE** (Linux CI; 실 API는 **NOT VERIFIED**) |
| M5 external plugin protocol, `kc plugin list`, [examples/kc-backend-demo](../../examples/kc-backend-demo) | **COMPLETE** (Linux CI) |
