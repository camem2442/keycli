---
status: accepted
date: 2026-10-24
---
# 0001 설계 선택

| 결정 | 이유 | 기각 대안 |
|---|---|---|
| 새 도구를 만든다 | 맥+헤드리스 Linux+샌드박스+키체인 링크+클립보드를 함께 해결하는 도구 없음 | envchain(맥 전용), 1Password op(유료·네트워크), `~/.secrets.env`(owner가 프로그램 원함) |
| Go, CGO 없음 | Linux 클라우드 필요, 단일 바이너리, 크로스 컴파일 | Swift(맥 전용), Rust(개인 도구에 부담), zsh(확장 한계) |
| 필요할 때 꺼내기 | 마찰 동일, 상시 노출 감소 | 셸 시작 시 전부 export |
| 키체인은 `/usr/bin/security` | 재빌드해도 ACL 유지, CGO 불필요 | 99designs/keyring 키체인(cgo 전용), 네이티브 API+Developer ID 서명 |
| 파일 저장소는 age | 순수 Go, 단순 | JOSE(keyring file), GPG(gopass) |
| `get`은 에이전트도 자유롭게 | 최우선 목표가 "바로 뽑아 쓰기" | TTY 전용 get(조사 제안) |
| 허용 규칙·감사·가림 제외 | 우회 가능해 마찰만 증가. 미들웨어 자리만 확보 | 처음부터 정책 계층 |
| 명령 `kc`, 저장소·패키지 `keycli` | 2026-10-24 SJ Mac에서 `kc` 명령·별칭·함수 없음, Homebrew formula/cask 없음 확인. 짧은 명령은 유지하고 검색·패키지 충돌은 긴 이름으로 회피 | `kc` 단독(검색 어려움, 향후 충돌 위험), 긴 이름 단독(입력 마찰) |
| 전송은 플랫폼 시크릿 → push → dotenv → age 순 | 공식 문서상 Cursor API·`gh secret set -f -`로 직접 등록 가능, 저장소 전체 동기화는 하나만 뚫려도 전부 노출 | 저장소 + `KC_KEY` 동기화, 웹 수동 입력만 |
| 네트워크는 `push`에만 | 조회는 샌드박스·오프라인에서도 동작해야 하고, 보내기는 사람이 맥에서 실행 | 전면 금지(push 불가), 전면 허용 |
| 보낼 곳은 저장소와 별도 `Target` | 쓰기 전용·원격·목록 의미가 달라 인터페이스 오염 방지 | 저장소에 `Push` 추가 |
| 확장은 외부 실행 파일 | 본체 경량, 언어 무관 | hashicorp/go-plugin(과함) |

## 결정된 질문

- 이름: 명령 `kc`, 저장소·패키지 `keycli`, 두 이름 모두 설치(위 표). 클라우드 Linux 이미지와 kubectl 별칭 충돌은 **NOT VERIFIED**; `kc doctor`가 PATH 충돌을 경고한다.

- 전송: [전송 계약](../contracts/transfer.md). Cursor 팀 단위 API·dotenv 일괄 붙여넣기는 **NOT VERIFIED**.

## 열린 질문

- workspace-infra 등록 시점.
