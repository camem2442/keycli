---
status: accepted
date: 2026-10-24
---
# 0004 외부 백엔드 플러그인

## 맥락

M5에서 1Password·Bitwarden 등 네트워크·GUI 저장소를 본체에 넣지 않고 붙일 수 있어야 한다. [0001](0001-design-choices.md)에서 hashicorp/go-plugin은 기각했다.

## 결정

| 항목 | 선택 | 이유 |
|---|---|---|
| IPC | subprocess + stdin/stdout JSON (v1) | docker-credential-helper와 유사, CGO·RPC 프레임워크 불필요 |
| 호출 단위 | 요청마다 새 프로세스 | 상태 누수·데드락 단순화, 타임아웃을 `exec`에 위임 |
| 활성화 | config `[backend.*]` + `type = "external"`만 | PATH 발견만으로는 **절대** 실행하지 않음 |
| 비밀 전달 | `value_base64` in JSON on stdin | argv·env·stderr 금지 ([AGENTS.md](../../AGENTS.md)) |
| 레지스트리 | 기존 `backend.Register("external")` | resolver 순서·`NeedsNetwork` 스킵과 동일 |
| 발견 | `kc plugin list`가 PATH + `~/.kc/plugins` 스캔 | enabled·handshake capabilities 표시 |

## 기각

- 장기 연결 세션: 타임아웃·버전 협상 복잡
- go-plugin / gRPC: 배포·CGO·본체 경량 목표와 충돌
- PATH 플러그인 자동 등록: 의도치 않은 코드 실행 위험

## 계약

[plugin-protocol.md](../contracts/plugin-protocol.md)
