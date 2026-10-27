---
status: accepted
date: 2026-10-27
---
# 0006 — OpenCode 실행 모드와 KC-22 배경 서비스

## 맥락

- v0.6.0에서 `kc wire opencode-vertex`와 `kc exec` env 주입으로 OpenCode가 `PROXY_API_KEY`(별칭 `vertex-byok-proxy`)를 쓰도록 배선했다 ([cli.md](../contracts/cli.md), [agents.md](../guides/agents.md)).
- KC-18: `opencode run --standalone` + `kc exec` 경로는 **COMPLETE** — 자식 프로세스가 env를 보므로 `{env:PROXY_API_KEY}`가 해석된다.
- KC-22 / PR [#11](https://github.com/camem2442/keycli/pull/11): `opencode run` **without** `--standalone`는 OpenCode **배경 서비스**가 Bearer를 해석한다. `kc exec` 자식 env는 데몬에 전달되지 않아 프록시가 `Missing Bearer`가 난다. PR #11은 `service.json`에 `PROXY_API_KEY`를 **영속 병합**하고 서비스를 재시작하는 방식으로 이를 맞춘다.
- [security-model.md](../contracts/security-model.md)는 **디스크에 평문 토큰이 남는 것**을 막는 것을 목표로 한다. OpenCode `~/.config/opencode/service.json`에 Bearer를 쓰는 것은 `0600`이어도 **평문 영속 저장**이며, `kc exec`가 사용자 몰래 공유 데몬 설정을 바꾸는 것은 **묵시적 mutation**이다.
- v0.7 로드맵 V07-A2 / KC-41: 실행 모드 **계약을 ADR로 고정**하고, KC-22는 **기본 경로에 넣지 않는다**.

## OpenCode 실행 모드 (3종)

| 모드 | 예시 호출 | Bearer 해석 위치 | `service.json` | v0.7 정책 |
|------|-----------|------------------|----------------|------------|
| **Standalone run** | `kc exec --only vertex-byok-proxy,PROXY_API_KEY -- opencode run --standalone …` | 같은 프로세스(standalone 서버) | **변경 없음** | **기본·권장** |
| **Standalone 대화형** | `kc exec … -- opencode --standalone` | 같은 프로세스 | **변경 없음** | 지원 (대화형) |
| **공유 배경 서비스** | `kc exec … -- opencode run …` (`--standalone` 없음) | `opencode service` 데몬 | OpenCode가 관리; **kc가 비밀을 쓰면 opt-in 계약 필요** | **기본 금지·opt-in만** |

공통 전제:

- `kc wire opencode-vertex`는 provider `apiKey`를 `{env:PROXY_API_KEY}`로 두며, **값은 kc 저장소 + `kc exec` 주입**이 정본이다.
- `kc exec` 자식은 시크릿을 읽을 수 있다 — 강한 OS 격리는 v0.7 범위 밖 ([security-model.md](../contracts/security-model.md)).

## 결정

### 1. 기본은 `--standalone`

- 문서·가이드·에이전트 규칙의 **OpenCode 예시는 `run --standalone`(또는 `--standalone`)을 전제**한다.
- `kc exec`가 `opencode`를 감쌀 때 **자동으로 non-standalone을 선택하거나 `--standalone`을 빼지 않는다**.
- KC-18 경로를 v0.7 **운영 기본**으로 유지한다.

### 2. 공유 배경 서비스는 opt-in만

non-standalone `opencode run`을 `kc exec`와 함께 쓰려면 **사람이 명시적으로 opt-in**해야 한다. 다음을 모두 만족할 때만 허용한다.

| 요구 | 내용 |
|------|------|
| 명시적 opt-in | 환경 변수 또는 CLI 플래그(구현명은 KC-22 PR에서 정함). **기본값 off.** |
| 고지 | opt-in 시 stderr/문서로 **평문 Bearer가 `service.json`에 남을 수 있음**, 공유 데몬 stop/restart, 동시 `opencode` 사용자 영향을 안내 |
| 범위 | `opencode run` non-standalone에만 한정; bare `opencode`, `service *`, `--version` 등은 sync 대상 아님 (PR #11 리뷰 정합) |
| argv | 비밀을 argv에 넣지 않음 ([AGENTS.md](../../AGENTS.md)); 파일 병합 시 로그·오류 출력 가림 |
| 인수 | Mac에서 non-standalone 1턴 + 서비스 복구 시나리오 **COMPLETE** 전에는 main에 sync 코드를 기본 on으로 넣지 않음 |

**묵시적 `service.json` 갱신은 금지.** `kc exec`만으로 배경 서비스 비밀이 영속되면 이 ADR 위반.

### 3. 권장 대안: 전용 서버 + `--server` (service.json 미변경)

배경 서비스와 동일 UX가 필요하면 **공유 전역 데몬 대신**, `kc exec`로 기동한 **전용 OpenCode 서버**와 클라이언트 `--server`를 우선한다.

```bash
# 예: 한 터미널 — env는 kc exec 자식에만 존재
kc exec --only vertex-byok-proxy,PROXY_API_KEY -- opencode serve --hostname 127.0.0.1 --port 4096

# 다른 터미널 — Bearer는 서버 쪽 env에만 필요; 클라이언트는 URL만
opencode run --server http://127.0.0.1:4096 …
```

- `service.json`을 kc가 건드리지 않는다.
- 수명은 사용자가 연 터미널/세션에 묶인다 (영속 데몬 비밀 누적 감소).

구체 플래그·하위 명령 이름은 OpenCode upstream CLI에 맞춘다; keycli는 **이 패턴을 문서 정본**으로 둔다.

### 4. PR #11 (KC-22) 처리

| 항목 | 결정 |
|------|------|
| **v0.6.0 / v0.7.0 기본 릴리스** | PR #11 **병합하지 않음** (현재 형태: `opencode run` 시 자동 service sync) |
| **PR #11 코드** | **Defer → v0.6.1 opt-in 경로**로 재스코프 후 **narrow merge** 후보. 자동 sync 제거, §2 opt-in·고지·인수 충족 시에만 병합 검토 |
| **병합 불가 조건** | opt-in 없음, 기본 on sync, Mac non-standalone 인수 미완, security-model과 모순되는 묵시 영속 저장 |
| **대안** | opt-in 구현이 지연되면 PR **close**하고 §3 `--server` 워크플로만 유지 (KC-22 기능은 DEFERRED, standalone은 이미 동작) |

브랜치 `cursor/kc22-opencode-service-env-0640`는 구현 참고·테스트 하네스로 유지할 수 있으나, **이 ADR 수용 전 main 병합은 하지 않는다** (사용자·KC-41 결정).

### 5. 문서·계약 정합

- [security-model.md](../contracts/security-model.md): OpenCode `service.json` 영속 Bearer는 **알려진 위험**; opt-in 시에만 사용자 책임 하에 허용.
- [planning/roadmap.md](../planning/roadmap.md) / HANDOFF: OpenCode 기본 경로는 standalone; KC-22는 opt-in 또는 DEFERRED로 표기.
- 구현 시 `internal/cli/exec.go`의 배경 sync 훅은 **기본 no-op**, opt-in일 때만 PR #11 유형 로직 실행.

## 기각

| 대안 | 이유 |
|------|------|
| non-standalone을 기본으로 두고 service sync를 항상 on | security-model·묵시 mutation·공유 데몬 운영 리스크 |
| PR #11을 계약 수정 없이 v0.6.1에 merge | 평문 영속·기본 경로 오염 |
| kc가 중앙 OpenCode 시크릿 서버 역할 | [0001](0001-design-choices.md) — keycli는 해석·전달 계층, 중앙 비밀 서버 아님 |
| `kc get PROXY_API_KEY`를 에이전트에 넘기기 | [agents.md](../guides/agents.md) — `kc exec` 기본 |

## 결과·검증

| 항목 | 상태 |
|------|------|
| ADR 본문 (KC-41) | **COMPLETE** |
| PR #11 병합 | **BLOCKED** (본 ADR 조건 미충족) |
| KC-22 opt-in 구현 + Mac non-standalone | **NOT VERIFIED** |
| Standalone OpenCode 1턴 (KC-18) | v0.6 인수 **COMPLETE** (프로젝트 기록) |
| §3 `--server` 워크플로 E2E | **NOT VERIFIED** |

## 관련

- GitHub PR [#11](https://github.com/camem2442/keycli/pull/11) — KC-22 (hold)
- 프로젝트: `keycli-v07-architecture-roadmap.md` § OpenCode, KC-41 / V07-A2
- 이전 결정: [0001](0001-design-choices.md) (네트워크·전송), [0005](0005-m6-audit-policy-middleware.md) (exec 경로 감사)
