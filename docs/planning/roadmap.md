---
status: proposed
---
# 로드맵

| 단계 | 내용 | 종료 조건 |
|---|---|---|
| M1 | cobra 골격, config, resolver, env·file 백엔드, `ls/get/exec/set/rm`, 종료 코드 | Linux에서 `verify.sh --suite unit` |
| M2 | keychain 백엔드(`security`), `link`, ai-core 읽기 어댑터 | macOS 수동 체크리스트 + 가짜 구현 테스트 |
| M3 | 클립보드 `cp`/`set --clip`, OSC 52, 자동 삭제 | 맥·SSH 수동 확인 |
| M4 | `setup-git`, `rules`, `doctor`, 에이전트별 설정 가이드 | Cursor/Claude Code/Codex/Antigravity 각 1회 실사용 |
| M4.5 | `push cursor`, `push gh`, `export --format dotenv`, age `export/import`, 원장·`status` | 테스트 환경 1개에 push 실사용, Linux에서 import 단위 테스트 |
| M5 | 외부 하위 명령·백엔드 프로토콜 | `kc plugin list`, 예제 백엔드 1개 |
| 이후 | 미들웨어(감사·허용·가림), 1Password 백엔드 | DEFERRED |
