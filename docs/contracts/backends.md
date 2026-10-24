---
status: proposed
---
# 저장소(백엔드)

| 종류 | 환경 | 쓰기 | 비고 |
|---|---|---|---|
| env | 모두 | 아니오 | Cursor 클라우드 Secrets 등 이미 주입된 환경변수. `ls`에는 별칭에 등록된 이름과 `KC_` 접두사만 표시 |
| file | 모두 | 예 | age 암호화 단일 파일, 0600, flock + 임시파일 rename. 키: macOS는 키체인 항목, Linux는 `KC_KEY` |
| keychain | macOS | 예(사람만) | `/usr/bin/security` 호출. 쓰기는 `-i` stdin. 항목별 첫 접근 시 "항상 허용" 1회. ACL에 `security`가 올라가므로 kc 재빌드 후 재확인 없음(**NOT VERIFIED**) |
| external | 모두 | 구현에 따름 | `kc-backend-<name>`; 1Password·Bitwarden 등. `NeedsNetwork` 선언 |

## 규칙

- 기본 순서 `env → file → keychain`. `NeedsNetwork` 저장소는 네트워크 차단 감지 시 건너뜀.
- keychain 실패가 샌드박스 때문이면 종료 코드 3과 원인 메시지.
- Linux에 keychain 링크가 있으면 "이 환경엔 키체인이 없음"(코드 3).
- ai-core 항목(`local.study.ai.<provider>`, 다중 슬롯)은 읽기 전용 어댑터로 노출. 슬롯 형식은 **NOT VERIFIED** — 구현 전 `ai-core/src/ai_core/keychain.py` 대조.
- Linux Secret Service·keyctl은 헤드리스에서 불안정하여 **DEFERRED**.
