# keycli (`kc`) — 에이전트 작업 규칙

## 공통 정책 바인딩

- 잠금 파일: [`dependencies/workspace-policy-lock.json`](dependencies/workspace-policy-lock.json)
- 로컬 바인딩: `../../../workspace-infra` (`~/Developer/programs/utils/keycli` 기준)
- `source_commit`: `451373fe0ca89ec92660387b435e9086514dec4e`
- 해시 불일치 시 자동 fallback 금지 → **BLOCKED**. infra 경로가 없으면 **NOT VERIFIED**.

상태 단어: **COMPLETE / PARTIAL / BLOCKED / DEFERRED / NOT VERIFIED**만 사용. 실행하지 않은 검사는 PASS가 아니다.

## 불변식

1. 비밀 값은 argv·로그·커밋·문서·테스트 fixture에 남기지 않는다. 키체인 쓰기는 `security -i` stdin으로 넘긴다.
2. 에이전트용 출력(`get` 값, `ls --json` 스키마 v1)과 종료 코드는 [CLI 계약](docs/contracts/cli.md)이 고정한다. 필드 **추가는 호환**으로 보아 `v`를 유지하고, 삭제·의미 변경만 `v`를 올린다.
3. 본체는 CGO 없이 동작한다. 로컬 저장소의 `ls/get/exec/cp`는 기본적으로 오프라인이다. 원격 비밀 조회 또는 사용은 사용자가 명시적으로 등록·승인한 경로에서만 허용한다. 대상·작업·권한을 제한하고 종단점 인증, 전송 보호, 시간 제한, 폐기 및 실패 시 차단을 설계·검증한다. 자동 연결이나 다른 원격 경로로의 무단 fallback은 금지한다. 이 정책 문구는 원격 기능이 이미 구현·검증되었다는 뜻이 아니다.
4. 실제 사용자 키체인·`~/.kc` 저장소를 테스트에서 건드리지 않는다. 키체인은 가짜 구현으로 Linux에서 테스트한다.
5. `ai-core`의 `local.study.ai.*` 키체인 항목은 **읽기만** 한다. 쓰기·삭제 금지.

## 검증 (구현 후 예정)

`bash verify.sh --suite <docs|policy|unit|backend|release|all>`. 변경 범위의 suite만 실행한다. Linux CI에서 `./verify.sh --suite all` 통과 시 해당 범위 **COMPLETE**; mac-smoke·실 Mac·서드파티 플러그인 실연동은 **NOT VERIFIED**.
