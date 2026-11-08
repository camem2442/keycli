# P4 — Cursor alias 경유 실 PUT 검증 (2026-11-08, macOS)

범위: `kc push cursor`가 **별칭(alias)으로 지정한 이름을 정본 이름으로 해석해** 실제 PUT하는지,
그리고 목록 확인·삭제까지 왕복이 되는지. v0.7.0에서 남은 "alias dry-run만 확인" 공백을 닫는다.

- 바이너리: `kc version v0.7.2-44-g0c94333 (0c94333)`
- 환경: Cursor Cloud env `52cae156-cf87-11f1-89ee-7aa6f0729b11`
- 값: **합성** (`pilot-alias-<epoch>`), 실제 자격증명 아님
- 정본 이름: `kc_pilot_alias_target` / 별칭: `kc_pilot_alias`

## 방법

1. 합성 시크릿을 정본 이름으로 저장 (`kc set --no-verify`).
2. 라이브 config 사본의 `[alias]`에 `kc_pilot_alias_target = ["kc_pilot_alias"]`만 추가하고
   `KC_CONFIG`로 분기 (정본 config는 변경하지 않음).
3. 별칭으로 `kc push cursor --dry-run` → 해석된 이름 확인.
4. 같은 별칭으로 **실제 PUT**.
5. 목록 API로 이름 확인 (값은 조회하지 않음).
6. `DELETE` 후 목록에서 사라졌는지 확인.
7. 로컬 합성 시크릿·임시 config 삭제.

## 결과

| 단계 | 결과 |
| --- | --- |
| dry-run 해석 | `kc: [dry-run] cursor push kc_pilot_alias_target → env 52cae156…` |
| 실 PUT | exit **0** |
| 목록 조회 | `['kc_pilot_alias_target']` — **별칭이 아니라 정본 이름** |
| DELETE | HTTP **200** |
| 삭제 후 목록 | `still_present=False` |
| 로컬 정리 | `kc rm` exit 0, 임시 config 제거 |

핵심: PUT된 이름이 `kc_pilot_alias`가 아니라 `kc_pilot_alias_target`이다. 즉 push 경로가
resolver의 canonical name을 사용한다.

## 한계

- 단일 환경(1개 env), 합성 값 1건. `--repos` 범위 제한·`secret_name_ambiguous`·상한(1000) 경로는 **NOT VERIFIED**.
- 삭제는 CLI 명령이 아니라 API 직접 호출로 수행했다(`kc push`에는 DELETE가 없다). 삭제 권한·경로는 이번에 확인했지만 `kc` 명령 표면에는 없다.
- Cursor Cloud GUI에서의 실제 주입(에이전트 실행 시 환경변수 노출)은 **NOT VERIFIED**.

## 상태

| 항목 | 상태 |
| --- | --- |
| alias → canonical 해석 (push 경로) | **COMPLETE** |
| 실 PUT → list → DELETE 왕복 | **COMPLETE** |
| 범위 제한·충돌·상한·GUI 주입 | **NOT VERIFIED** |
