---
status: proposed
---
# 저장소(백엔드)

| 종류 | 환경 | 쓰기 | 비고 |
|---|---|---|---|
| env | 모두 | 아니오 | Cursor 클라우드 Secrets 등 이미 주입된 환경변수. `ls`에는 별칭에 등록된 이름과 `KC_` 접두사만 표시 |
| file | 모두 | 예 | age 암호화 단일 파일, 0600, flock + 임시파일 rename. 키: macOS는 키체인 항목 `kc.age`/`kc`, Linux는 `KC_KEY` |
| keychain | macOS | 예(사람만) | `/usr/bin/security` 호출. 쓰기는 `security -i` + hex `-X`(argv에 평문 없음). `-T /usr/bin/security`로 ACL 고정 |
| external | 모두 | 구현에 따름 | `kc-backend-<name>`; 1Password·Bitwarden 등. `NeedsNetwork` 선언 |

## macOS Keychain ACL

- **kc가 만든 항목**(`kc.secret.*`, `kc.age` 등): `add-generic-password` 시 `-T /usr/bin/security`를 넣어 **신뢰 주체가 `security(1)`만**이 되게 한다. 이후 읽기는 `security find-generic-password -w`로 이루어지므로 **kc 바이너리를 재빌드해도** “항상 허용”을 kc마다 다시 누를 필요가 없다.
- **다른 앱이 만든 항목**(ai-core `local.study.ai.*`, 레거시 lecturetranscriber 등): kc는 **읽기 전용**이다. 최초 읽기 때 macOS가 **한 번** “Always Allow”를 물을 수 있다(실기 **NOT VERIFIED**는 `verify.sh --suite mac-smoke`).
- **age 키(`kc.age`)**: 사람이 `kc keygen` 등으로 만든 identity를 키체인에 **한 번** 저장한다. 이후 file 백엔드가 같은 `-T` 규칙으로 읽는다.

## 규칙

- 기본 순서 `env → file → keychain`. `NeedsNetwork` 저장소는 네트워크 차단 감지 시 건너뜀.
- keychain 실패가 샌드박스 때문이면 종료 코드 3과 원인 메시지.
- Linux에 keychain 링크가 있으면 "이 환경엔 키체인이 없음"(코드 3). Linux CI는 `cmd/fakesecurity` + `allow_test_hooks`로 에뮬레이션.
- ai-core 항목은 [0003](../decisions/0003-aicore-keychain.md) 읽기 어댑터로만 노출(자동 이전 없음).
- Linux Secret Service·keyctl은 헤드리스에서 불안정하여 **DEFERRED**.
