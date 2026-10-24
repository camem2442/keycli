---
status: accepted
---
# 0005 — M6 감사·정책 미들웨어

## 맥락

에이전트가 `kc get`/`exec`으로 시크릿을 꺼낼 때 **추적 가능성**과 **과도한 이름 읽기 방지**가 필요하다. M1부터 `internal/middleware.Lookup` 타입만 있었고, M6에서 첫 두 구현을 resolver에 연결한다.

## 결정

1. **체인 위치** — 모든 값 읽기는 `Resolver.Get`/`GetWithSource`의 `middleware.Chain`을 통과한다. 순서는 `config.middleware` 배열(첫 항목이 가장 바깥). 비어 있으면 체인 없음 = 기존 동작.
2. **감사** — append-only JSONL, 기본 `~/.kc/audit.log`, `0600`, `max_bytes` 초과 시 `audit.log.1`로 단일 회전. 레코드: ts, command, name, source, outcome, agent, pid, ppid, cwd. `audit.required=true`일 때만 쓰기 실패가 성공한 읽기를 막는다.
3. **정책** — `[policy.agent.<id>] allow = [...]`만 deny-by-default. 정책 섹션 전체가 없으면 전부 허용. 거부는 `kcerrors.ExitDenied`(5)와 audit `outcome=denied`.
4. **에이전트 식별** — `KC_AGENT` 우선, 그다음 환경 힌트(Cursor/Claude/Codex/CI/TTY `human`). 표는 [agents.md](../guides/agents.md).

## 대안

- 정책을 커널/샌드박스 수준으로 강제 — 같은 UID 에이전트에는 의미 없어 거부.
- 감사를 syslog로 — 로컬 파일이 에이전트·사람 모두에게 단순.

## 결과

- Linux `./verify.sh --suite all` **COMPLETE**
- mac-smoke·실 에이전트 정책 운영은 **NOT VERIFIED**
