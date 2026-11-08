# P4 — `kc push cloudflare` 실 경로 검증 (2026-11-08, macOS)

범위: `kc push cloudflare`가 **실제 Worker에 시크릿을 넣는지**를 버리는 Worker로 확인한다.
프로덕션 Worker 회전은 하지 않는다.

- 바이너리: `kc version v0.7.2-44-g0c94333 (0c94333)`
- 계정: `b398955bcfe917a4af61d179af967b6a` (Development Infrastructure)
- Worker: `kc-push-pilot-1794120569` (일회성, 종료 후 삭제)
- 값: **합성** (`pilot-cf-<epoch>`)

## 방법

1. `cloudflare_agent`로 **일회성 Editor 토큰**을 발급 (`Workers Scripts Write`, account 범위).
2. 최소 Worker를 그 토큰으로 배포 (`workers_dev=false`, 라우트 없음).
3. 합성 시크릿을 `kc set`으로 저장.
4. `kc push cloudflare <NAME> --worker <W> --wrangler-config <path>` 실행.
   - 내부적으로 `wrangler secret put NAME --name W --config path`에 **stdin으로 값 전달**.
5. Worker의 `GET .../secrets`로 이름만 확인.
6. Worker 삭제 → 시크릿도 함께 사라짐. 토큰 삭제. 로컬 시크릿 삭제.

## 결과

| 단계 | 결과 |
| --- | --- |
| Editor 토큰 발급 | success, id `94d8558e…` |
| Worker 배포 | version `1b372c68-a8a0-48f1-bf9b-878ff7132301` |
| `kc push cloudflare` | **exit 0** |
| Worker secrets | `['kc_push_pilot_secret']` |
| Worker 삭제 후 조회 | **404** |
| Editor 토큰 삭제 | HTTP 200 (발급 주체로 수행) |
| 남은 계정 토큰 | `['dev_cf_worker_read']` 1건 |
| 로컬 정리 | `kc rm` exit 0, 임시 파일 삭제 |

## 운영상 발견

- **Editor 토큰은 자기 자신을 삭제할 수 없다** (`DELETE /tokens/{id}` → 403). 삭제는
  `Account API Tokens Write`를 가진 발급 주체(`cloudflare_agent`)로 수행해야 한다.
  일회성 토큰을 쓰는 절차에는 "삭제 권한을 가진 주체"를 반드시 함께 지정해야 한다.
- `wrangler secret put`은 **즉시 배포**를 수반한다. 그래서 이번 검증은 프로덕션 Worker가
  아니라 일회성 Worker에서 수행했다. 브리프의 "프로덕션 회전 금지"를 지킨 방식이다.

## 한계

- 단일 Worker, 합성 값 1건. `--worker-env`, `--repos` 상당 범위, 대량 시크릿, 롤백 경로는 **NOT VERIFIED**.
- 무료 플랜 한도·반복 실행 예산은 측정하지 않았다.
- `kc push`는 삭제 표면이 없다. 정리는 `wrangler delete`와 API 호출로 수행했다.

## 상태

| 항목 | 상태 |
| --- | --- |
| `kc push cloudflare` 실 PUT (stdin 경유) | **COMPLETE** |
| Worker 삭제로 시크릿 회수 | **COMPLETE** |
| 일회성 토큰 발급·회수 | **COMPLETE** |
| 범위·한도·롤백·다중 시크릿 | **NOT VERIFIED** |
