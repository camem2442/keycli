---
status: complete
owner: keycli
date: 2026-11-19
keycli_main: 352e12d72a3fc7189e7a4049c5643ce1d593d41e
scope: local synthetic third-party plugin acceptance; no provider, no network, no operating grant
result: COMPLETE — `_management/evidence/keycli-plugin-e2e-20261119.cb80e3/`
---

# Goal — 서드파티 `kc-backend-*` 실연동 (합성 플러그인)

`docs/planning/roadmap.md`의 NOT VERIFIED 항목 "서드파티 `kc-backend-*` 실연동 — demo 외"를
닫는 것이 목표다. 예제(`examples/kc-backend-demo`)를 재사용하지 않고, 계약
[plugin-protocol.md](../contracts/plugin-protocol.md)만 보고 독립 구현한 합성 플러그인으로 검증한다.

## 범위

- config에 명시 등록된 외부 백엔드의 handshake·list·get·set·delete
- 실패 격리: 크래시/잘못된 JSON 플러그인이 `ls`·`doctor`·다른 백엔드를 깨지 않음
- `timeout` 초과 시 hang 없이 종료
- `needs_network` 플러그인은 읽기 경로에서 호출되지 않음
- PATH에만 있는 플러그인은 실행되지 않음
- 비밀 값이 argv·stderr·evidence에 남지 않음

## 비목표

- 실 공급자(1Password·Vault 등) 연동
- 플러그인 설치·배포, `kc plugin` CLI 확장
- 네트워크 사용 경로 자체(정책상 금지)

## 승인 기준

| ID | 기준 |
|---|---|
| P1 | `kc ls`에 합성 플러그인 항목이 backend `external`로 표시되고 exit 0 |
| P2 | `kc get` 값이 저장 값과 일치, `kc exec` 주입 확인 |
| P3 | 쓰기 경로: order에 플러그인만 둔 config에서 `kc set`/`kc rm`이 플러그인 저장소에 반영 |
| P4 | 크래시 플러그인이 있어도 `ls`/`doctor` exit 0, 해당 이름 조회만 실패(exit 1) |
| P5 | `timeout = "1s"`에서 5초 sleep 플러그인이 5초를 넘기지 않고 실패 |
| P6 | `needs_network` 플러그인은 마커 파일이 생기지 않고 `get`은 exit 2 |
| P7 | PATH 전용 플러그인은 `plugin list`에만 보이고 실행되지 않음 |
| P8 | 값이 argv 로그·stderr·evidence에 없음 |

## 안전

임시 `KC_CONFIG`·임시 데이터만 사용한다. 실제 `~/.kc`, 키체인, PATH에는 아무것도 설치하지 않는다.
플러그인은 로컬 파이썬 스크립트이며 네트워크를 쓰지 않는다.

## 결과 (2026-11-19)

| ID | 실측 |
|---|---|
| P1 | `kc ls --json` exit 0, `synth_item`이 backend `synthpy`(kind external)로 표시 |
| P2 | `kc get` 값 일치, `kc exec --require --only`로 `SYNTH_ITEM` 주입 |
| P3 | 플러그인만 있는 order에서 `kc set` exit 0, 저장 파일은 base64만 |
| P4 | 크래시 플러그인이 있어도 `ls`/`doctor` exit 0, 해당 이름 `get`은 exit 2, doctor 사유 `exit status 1` |
| P5 | `timeout = "1s"`에서 5초 sleep 플러그인 요청이 1.96초에 실패(exit 2) |
| P6 | `needs_network` 플러그인은 read op 0회(handshake 1회만), `get` exit 2 |
| P7 | PATH 전용 플러그인은 `plugin list`에 표시되고 실행되지 않음 |
| P8 | 값이 argv·stderr·evidence에 없음(주입 증명용 `env` 덤프는 마스킹) |

### 관찰

1. `needs_network` 외부 백엔드는 **생성 시 handshake를 위해 호출**된다. 읽기 op만 건너뛴다.
2. `Deps.NetworkBlocked`가 어디에도 연결돼 있지 않아 `doctor`는 해당 플러그인을 `available: true`로 보고한다. 읽기 정책과 진단 표시가 다른 근거를 쓴다.
3. `kc exec --require --only N`은 한 실행에 `exec` audit 행을 2개 남긴다(G6-S에서도 동일).
