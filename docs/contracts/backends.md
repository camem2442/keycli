---
status: proposed
---
# 저장소(백엔드)

| 종류 | 환경 | 쓰기 | 비고 |
|---|---|---|---|
| env | 모두 | 아니오 | Cursor 클라우드 Secrets 등 이미 주입된 환경변수. `ls`에는 별칭에 등록된 이름만 표시하며, `KC_*` 내부 변수는 목록·조회 대상이 아니다 |
| file | 모두 | 예 | age 암호화 단일 파일, 0600, 전용 `.lock` flock + 임시파일 rename. read-modify-write는 프로세스 간 직렬화. 키: `KC_KEY` → `~/.kc/receiver.key`(0600) → macOS 키체인 항목 `kc.age`/`kc` |
| keychain | macOS | 예(사람만) | `/usr/bin/security` 호출. 쓰기는 `security -i` + hex `-X`(argv에 평문 없음). `-T /usr/bin/security`로 ACL 고정 |

## keychain 이름 규칙

- 쓰기 이름은 항상 `kc.secret.<name>` 서비스로 저장한다. 점이 있는 이름(`foo.bar`)도 예외가 아니며, `kc.secret.` 접두사를 이미 붙인 이름은 그대로 쓴다.
- 이름은 `[A-Za-z0-9][A-Za-z0-9._:#-]*`만 허용한다. 공백·따옴표·역슬래시·개행은 `security -i` 명령 주입을 막기 위해 거부한다.
- `kc.age`는 age identity 전용으로 예약되어 `set`/`rm`/`get`에서 거부한다.
- `ls`는 `kc.secret.*` 중 유효한 이름만 표시한다.
| external | 모두 | 구현에 따름 | `kc-backend-<name>`; config에만 등록·`enabled` 시 실행. [플러그인 프로토콜](plugin-protocol.md). PATH 발견만으로는 실행 안 함 |

## macOS Keychain ACL

- **kc가 만든 항목**(`kc.secret.*`, `kc.age` 등): `add-generic-password` 시 `-T /usr/bin/security`를 넣어 **신뢰 주체가 `security(1)`만**이 되게 한다. 이후 읽기는 `security find-generic-password -w`로 이루어지므로 **kc 바이너리를 재빌드해도** “항상 허용”을 kc마다 다시 누를 필요가 없다.
- **다른 앱이 만든 항목**(ai-core `local.study.ai.*`, 레거시 lecturetranscriber 등): kc는 **읽기 전용**이다. 최초 읽기 때 macOS가 **한 번** “Always Allow”를 물을 수 있다(실기 확인은 `verify.sh --suite mac-smoke` — 2026-10-25 통과). mac-smoke는 **실제 `HOME`**을 쓰고 `KC_CONFIG`만 격리한다 — 임시 `HOME`은 “키체인 발견할 수 없음”을 유발한다.
- **age 키(`kc.age`)**: 사람이 `kc init --print-identity` 등으로 얻은 identity를 키체인에 **한 번** 저장한다. 이후 file 백엔드가 같은 `-T` 규칙으로 읽는다.

## 규칙

- 기본 순서 `env → file → keychain`. `NeedsNetwork` 저장소는 값 읽기·목록(`ls/get/exec/cp`)에서 건너뛴다(본체는 읽기 경로에서 네트워크를 쓰지 않는다).
- keychain 실패가 샌드박스 때문이면 종료 코드 3과 원인 메시지. 샌드박스 안 `security`는 접근 차단을 exit 44 + `SecKeychainSearchCreateFromAttributes: ... not valid`로 보고하므로, 이 stderr를 만나면 “항목 없음”(2)이 아니라 **차단(3)**으로 매핑한다(`internal/backend/keychain/errors.go`).
- Linux에 keychain 링크가 있으면 "이 환경엔 키체인이 없음"(코드 3). Linux CI는 `cmd/fakesecurity` + `allow_test_hooks`로 에뮬레이션.
- ai-core 항목은 [0003](../decisions/0003-aicore-keychain.md) 읽기 어댑터로만 노출(자동 이전 없음).
- Linux Secret Service·keyctl은 헤드리스에서 불안정하여 **DEFERRED**.
