# 2026-11-08 정책·전송 계층 마감 (PR #30–#39)

하루 동안 정책 계층과 전송 경로를 닫은 범위를 한 곳에 모은다. 이 문서는 **요약**이며,
각 항목의 정본은 연결된 영수증이다.

- 기준 바이너리: `kc version v0.7.2-50-g513414b (513414b)`
- 저장소: `camem2442/keycli`
- 모든 PR은 `main`에 머지 커밋으로 병합됨

## 1. PR 목록

| PR | 결과 | 핵심 |
| --- | --- | --- |
| [#30](https://github.com/camem2442/keycli/pull/30) | `b69c3b6` | `[policy.agent.*] deny` 추가, 잘못된 글로브는 조용히 무시되지 않음 |
| [#31](https://github.com/camem2442/keycli/pull/31) | `b5ccc69` | Mac 라이브 적용 검증([영수증](2026-11-08-policy-deny-live-enforcement.md)) |
| [#32](https://github.com/camem2442/keycli/pull/32) | `fc5bf75` | 잘못된 글로브가 진단 명령까지 막지 않게 (명령 단위 거부 + fail-closed) |
| [#33](https://github.com/camem2442/keycli/pull/33) | `043029f` | 거부 사유 `deny_hit`/`not_allowed`/`invalid_pattern` + 매칭 규칙 노출 |
| [#34](https://github.com/camem2442/keycli/pull/34) | `4de3d12` | 증거 4경로 보완, "필드 추가는 v1 유지" 규칙을 AGENTS.md에 고정 |
| [#35](https://github.com/camem2442/keycli/pull/35) | `c6e931b` | Cursor Cloud allowlist 파일럿([영수증](2026-11-08-kc-cursor-cloud-p3-01.md)) |
| [#36](https://github.com/camem2442/keycli/pull/36) | `0c94333` | `kc policy show` / `validate` |
| [#37](https://github.com/camem2442/keycli/pull/37) | `496b89e` | Cursor 별칭 경유 실 PUT → 목록 → DELETE([영수증](2026-11-08-kc-p4-cursor-alias-put.md)) |
| [#38](https://github.com/camem2442/keycli/pull/38) | `d62b781` | `kc push cloudflare` 일회성 Worker([영수증](2026-11-08-kc-p4-cloudflare-push.md)) |
| [#39](https://github.com/camem2442/keycli/pull/39) | `513414b` | import 덮어쓰기 거부 + 만료·회전 소유권([문서](2026-11-08-credential-lifecycle-ownership.md)) |

## 2. 지금 실제로 되는 것

| 기능 | 확인 |
| --- | --- |
| `deny`가 `allow`보다 먼저 평가 | 단위 + Mac 라이브 |
| 거부 시 자식 프로세스 미실행 | Mac + Cursor Cloud VM |
| 거부 사유·매칭 규칙 노출 | `ls --json`, `policy check`, audit |
| 잘못된 글로브 | 진단 명령 외 전부 exit 1, 평가는 fail-closed |
| `policy show` / `validate` | 구현 + 단위 테스트 |
| Cursor alias → canonical PUT | 실 환경 왕복 |
| `kc push cloudflare` | 일회성 Worker 실 PUT 후 삭제 |
| import 덮어쓰기 | `--force` 없으면 거부, 만료 없음 경고 |

## 3. NOT VERIFIED (덮지 않은 것)

- **다중 에이전트 동시 적용**, Codex/Claude 1턴, Mac GUI/TUI 네이티브 인수
- Cursor Cloud: `policy_digest` 재현, `pin_match`, GUI 주입, `--repos` 범위
- Cloudflare: `--worker-env`, 다중 시크릿, 롤백, 무료 플랜 한도
- `deny` 기능은 릴리스 `v0.7.2` 바이너리에 **없음** — 파일럿은 allowlist 계약만 검증했다

## 4. 운영상 발견

1. **정책 allowlist는 보안 경계가 아니다** — `KC_AGENT`는 같은 UID가 바꿀 수 있다. 경계는 토큰 scope·만료다.
2. **진단 도구가 막히면 복구가 막힌다** — 설정 오류는 명령 단위로 거부하고 `doctor`/`policy`는 남긴다.
3. **일회성 Editor 토큰은 자기 삭제가 403** — 회수는 발급 주체가 해야 한다.
4. **`wrangler secret put`은 즉시 배포** — 검증은 일회성 Worker에서.
5. **import는 만료를 싣지 않는다** — 회전 소유자를 사람이 명시해야 한다.

## 5. 남은 단계

| 단계 | 상태 |
| --- | --- |
| P5 검증 통합 (receipt `policy_digest`, workspace-infra 연결) | 대기 |
| P6 v0.8.0 제안 | P3·P4 영수증 이후 |

## 6. 참고: 별개 스레드

DEV-CF Secret 이관(Issue #200, `dev_cf_worker_read`, gateway 3/15 staging)은 이 문서의 범위가
아니다. 해당 토큰은 2026-11-09T06:56Z 만료이며, 재발급 절차는
[소유권 문서](2026-11-08-credential-lifecycle-ownership.md)에 있다.
