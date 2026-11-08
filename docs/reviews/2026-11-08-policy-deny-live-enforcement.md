# policy deny — 라이브 적용 검증 (2026-11-08, macOS)

범위: `kc`의 `allow`/`deny` 규칙이 **실제 읽기 경로**(`get`/`exec`/`ls`/`policy check`)에서
적용되는지 확인한다. 값은 출력하지 않았고, 실제 자격증명 대신 **버리는 probe secret**을 썼다.

- 바이너리: `kc version v0.7.2-32-gb69c3b6 (b69c3b6)`
- 호스트: `SJ-Mac.local`, Darwin 27.2.0 arm64
- 에이전트: `KC_AGENT=cursor`
- 정책 정본: `~/.config/kc/config.toml` (`allow = ["*"]` + deny 목록)

## 방법

`dev_policy_probe`를 가짜 값으로 저장하고(`kc set --no-verify`), 세 가지 설정으로 같은 읽기를 반복했다.

1. **A** — 라이브 설정 (probe는 deny 목록에 없음)
2. **B** — 라이브 설정의 `[policy.agent.cursor] deny`에 `dev_policy_probe` 추가
3. **C** — 같은 위치에 잘못된 글로브 `dev_policy_probe[` 추가

A/B/C는 `KC_CONFIG`로만 분기했고 정본 파일은 건드리지 않았다.

## 결과

### A. 허용 (probe 미차단)

| 명령 | 결과 |
| --- | --- |
| `kc get dev_policy_probe` | exit 0 |
| `kc exec --require --only dev_policy_probe -- true` | exit 0 |
| `kc ls --json` | `policy_denied=false` |
| `kc policy check dev_policy_probe` | exit 0 |

### B. deny 적용

| 명령 | 결과 |
| --- | --- |
| `kc get dev_policy_probe` | **exit 5** |
| `kc exec --require --only dev_policy_probe -- true` | **exit 5** (자식 프로세스 시작 전 차단) |
| `kc ls --json` | **`policy_denied=true`** |
| `kc policy check dev_policy_probe` | **exit 5** |

### C. 잘못된 deny 글로브 — 네 경로 전부

측정 바이너리: `kc version v0.7.2-38-g043029f (043029f)`.
설정은 라이브 사본의 `[policy.agent.cursor] deny`에 `bad[`를 추가한 것이다.

| 명령 | 결과 | 메시지 |
| --- | --- | --- |
| `kc get github_agent` | **exit 1** | `Error: policy.agent.cursor.deny: invalid glob "bad[": syntax error in pattern` |
| `kc exec --require --only github_agent -- true` | **exit 1** | 동일 (자식 미실행) |
| `kc ls` | **exit 1** | 동일 |
| `kc policy check github_agent` | **exit 1** | `kc: policy.agent.cursor.deny: invalid glob "bad[": …` |
| `kc rules` (진단) | exit 0 | — |

조용히 무시되지 않고, 에이전트·목록·패턴을 지목해 실패한다.

이전 빌드(`b69c3b6`, PR #31 시점)에서는 같은 명령들이 **설정 로드 단계**에서 exit 1로
끝났다. 위 표는 PR #32 이후 빌드로, 실패가 명령 단위로 보고되는 동작을 측정한 것이다.
두 빌드 모두 네 경로가 exit 1이라는 결론은 같지만 실패 지점이 다르다.

## 정리

- `kc rm dev_policy_probe` 완료, 목록에 잔존 없음(총 20개).
- 정본 설정 파일은 변경하지 않았다(임시 사본은 종료 시 삭제).

## 이 검증이 증명하는 것 / 아닌 것

증명:

- `deny`가 `get`/`exec`/`ls`/`policy check` 네 경로에서 동일하게 적용된다.
- `kc exec --require --only`는 정책 거부 시 **자식 프로세스를 시작하지 않는다**.
- 잘못된 글로브는 로드 단계에서 명시적으로 거부된다.

증명하지 않음:

- 보안 경계로 쓰이는 것. `KC_AGENT`는 같은 UID 프로세스가 바꿀 수 있으므로 이 정책은 **실수 방지 가드레일**이다.
- 다중 에이전트 동시 적용, Codex/Claude 실 1턴, Cloud Agent VM, `kc push` 경로.
- 실제 자격증명 대상 동작 — probe는 합성 값이다.

## 상태

| 항목 | 상태 |
| --- | --- |
| Mac 로컬 `cursor` 정책 적용 (get/exec/ls/check) | **COMPLETE** (이 범위) |
| 잘못된 글로브 거부 | **COMPLETE** |
| 다중 에이전트·Cloud·push E2E | **NOT VERIFIED** |
