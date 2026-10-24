---
status: accepted
---
# 외부 백엔드 플러그인 프로토콜 (v1)

`kc`는 config에 **명시적으로 등록된** `kc-backend-<name>` 실행 파일만 호출한다. PATH에서 발견만 된 플러그인은 `kc plugin list`에 표시되지만 **자동 실행되지 않는다**.

## 실행 모델

- 호출당 **한 번** subprocess를 띄운다.
- **stdin**: 한 줄 JSON 요청 + `\n`
- **stdout**: 한 줄 JSON 응답 + `\n` (비밀 값은 여기만)
- **stderr**: 사람용 로그 (비밀 금지)
- **argv·환경변수**: 비밀 금지. `kc`는 `PATH`만 넘긴다.

기본 타임아웃 30초. `[backend.NAME]`의 `timeout = "5s"`로 조정.

## 요청 (`v` = 1)

| `op` | 필드 | 설명 |
|---|---|---|
| `handshake` | `backend` | config 인스턴스 이름 |
| `list` | `backend` | |
| `get` | `backend`, `name` | |
| `set` | `backend`, `name`, `value_base64`, `note`, `expires` | `expires`는 `YYYY-MM-DD` 또는 null |
| `delete` | `backend`, `name` | |

## 응답

성공:

```json
{"v":1,"ok":true,"protocol":1,"capabilities":{"can_write":true,"can_list":true,"needs_network":false,"interactive":false}}
{"v":1,"ok":true,"items":[{"name":"x","note":"","expires":null}]}
{"v":1,"ok":true,"value_base64":"..."}
```

실패 (`ok: false`):

| `errno` | kc 종료 코드 |
|---|---|
| `not_found` | 2 |
| `blocked` | 3 |
| `locked` | 4 |
| `denied` | 5 |
| 기타 | 1 |

플러그인 프로세스는 프로토콜 오류 시에도 **0**으로 종료하고 `ok:false`를 반환하는 것을 권장한다. 크래시·타임아웃·잘못된 JSON은 kc가 1로 처리한다.

## 바이너리 위치

1. `command`가 절대/상대 경로면 해당 파일
2. `command`가 이름만이면 `~/.kc/plugins/<name>` → PATH
3. `plugin = "demo"`면 `kc-backend-demo`를 같은 순서로 탐색

## config 예시

```toml
order = ["link", "env", "store", "vault"]

[backend.vault]
type = "external"
plugin = "1password"
timeout = "15s"
enabled = true
```

예제 구현: [examples/kc-backend-demo](../../examples/kc-backend-demo/main.go)
