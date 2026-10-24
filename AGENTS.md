# keycli (`kc`) — 에이전트 작업 규칙

## 공통 정책 바인딩

- 잠금 파일: [`dependencies/workspace-policy-lock.json`](dependencies/workspace-policy-lock.json)
- 로컬 바인딩: `../../../workspace-infra` (`~/Developer/programs/utils/keycli` 기준)
- `source_commit`: `ae5e8378a652cd6d55e28d2bcdf7d113a106b251`
- 해시 불일치 시 자동 fallback 금지 → **BLOCKED**. infra 경로가 없으면 **NOT VERIFIED**.

상태 단어: **COMPLETE / PARTIAL / BLOCKED / DEFERRED / NOT VERIFIED**만 사용. 실행하지 않은 검사는 PASS가 아니다.

## 불변식

1. 비밀 값은 argv·로그·커밋·문서·테스트 fixture에 남기지 않는다. 키체인 쓰기는 `security -i` stdin으로 넘긴다.
2. 에이전트용 출력(`get` 값, `ls --json` 스키마 v1)과 종료 코드는 [CLI 계약](docs/contracts/cli.md)이 고정한다. 바꾸면 버전을 올린다.
3. 본체는 CGO 없이 동작한다. **값을 읽는 경로(`ls/get/exec/cp`)는 네트워크를 쓰지 않는다.** 네트워크는 사람이 실행하는 보내기 명령(`push`)의 `internal/target/` 안에서만 허용한다. 네트워크가 필요한 저장소는 외부 `kc-backend-<name>`으로만 붙인다.
4. 실제 사용자 키체인·`~/.kc` 저장소를 테스트에서 건드리지 않는다. 키체인은 가짜 구현으로 Linux에서 테스트한다.
5. `ai-core`의 `local.study.ai.*` 키체인 항목은 **읽기만** 한다. 쓰기·삭제 금지.

## 검증 (구현 후 예정)

`bash verify.sh --suite <docs|policy|unit|backend|release|all>`. 변경 범위의 suite만 실행한다. Linux CI에서 `./verify.sh --suite all` 통과 시 해당 범위 **COMPLETE**; mac-smoke·실 Mac·서드파티 플러그인 실연동은 **NOT VERIFIED**.
