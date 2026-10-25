---
status: proposed
---
# 보안 모델

**kc의 목표는 마찰 제거와 실수 방지다. 같은 사용자로 도는 에이전트를 막는 경계가 아니다.**

## 막는 것

- 디스크·백업·git에 평문 토큰이 남는 것
- 복사·붙여넣기 과정의 노출, 클립보드 잔류(30초 자동 삭제)
- argv를 통한 노출(키체인 쓰기 stdin, `exec` 자식에서 `KC_KEY`/`KC_AGENT` 제거, 클립보드 자동 삭제 checksum은 stdin)

## 막지 못하는 것

- `KC_KEY`나 키체인 허용을 가진 에이전트는 모든 값을 꺼낼 수 있다.
- kc를 거치지 않는 접근, `exec -- sh -c 'echo $X'` 류 우회.
- 프롬프트 인젝션으로 인한 유출.
- `/usr/bin/security`에 허용한 항목은 같은 사용자의 모든 프로세스가 읽을 수 있다.

## 그래서 지킬 것

- 에이전트용 토큰은 권한을 좁게(GitHub fine-grained, 저장소 단위), 만료를 짧게.
- 결제·계정 관리 같은 고위험 비밀은 kc에 넣지 않는다.
- 저장은 사람만, 에이전트는 읽기.
- **감사 로그**(`audit` 미들웨어): 읽기 시도 메타만 append(JSONL). 값·해시·길이 없음.
- **허용 규칙**(`policy` 미들웨어): 에이전트별 allowlist 글로브. **실수 방지용 가드레일이며 보안 경계가 아님** — 같은 사용자 프로세스는 `KC_AGENT`를 바꿀 수 있다. 강화(서명된 에이전트 ID, 별도 OS 사용자 등)는 문서만 제안, 미구현.
- 출력 가림·1Password 백엔드 등은 **DEFERRED**.

## 신뢰 경계: config

`~/.config/kc/config.toml`은 **사람이 소유한 신뢰 입력**이다. 다음 두 가지는 config를 쓸 수 있는 주체에게 실행 권한을 준다.

- `[git."HOST"].command`: `kc git-credential get`이 실행하는 셸 명령. secret이 env로 주입된 상태로 실행되므로, config를 고칠 수 있는 주체는 그 secret을 얻는다.
- `[backend.*]` external 플러그인: config에 등록된 `kc-backend-*`만 실행.

따라서 config 파일 권한은 `0600`(kc가 저장할 때 기본)으로 두고, 저장소·백업·동기화 대상에 넣지 않는다. 자동 탐색(예: PATH만으로 명령 실행)은 하지 않는다.

## 알려진 환경 제약

- Codex/Claude Code macOS 샌드박스에서 키체인 접근 실패 보고(openai/codex#24204, anthropics/claude-code#44195) → file/env 경로로 대체.
- 서명 없는 재빌드는 바이너리 기준 키체인 ACL을 무효화 → `security` CLI 경유로 회피.
