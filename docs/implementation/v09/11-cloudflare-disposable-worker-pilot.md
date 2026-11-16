---
status: NOT VERIFIED
date: 2026-11-10
owner: keycli
scope: disposable-Cloudflare-acceptance-plan-only
---

# K09-04B — 일회성 Cloudflare Worker 운영 인수 계획

## 현재 소스 기준과 비인가 효과

- [KeyCLI #50](https://github.com/camem2442/keycli/pull/50) 후속 수정 HEAD `adcf80ed98f2b6ed2836a6947ee44d33768be3ee`: `versions secret put` · metadata GET · journal/reconcile. 명령은 low-level Go API이며 일반 운영 CLI 활성화가 아니다.
- [검증용 통합 PR #54](https://github.com/camem2442/keycli/pull/54) HEAD `778207e385dcad0eda3541176e07ef9694481827`: Go 1.22.12 Linux `verify.sh --suite all` [SUCCESS](https://github.com/camem2442/keycli/actions/runs/42766043060). #54는 **DO NOT MERGE** 연습용; #50 독립 브랜치의 같은 수정된 Git blob은 아직 exact-head 자체 CI가 아니다.
- Cloudflare 공식 [versions list API](https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/versions/methods/list/)는 `result.items` 객체를 반환한다. #50 원래의 bare array 디코딩은 실패하므로 수정했다.
- 이번 문서는 계획만이다. 실제 API 토큰 발급, Workers 생성·Bootstrap, secret put, deploy, deletion은 **수행하지 않았다**.

## 실계정 파일럿의 명시적 소유·격리

- 대상: Development Infrastructure **내의 신규 disposable Worker 하나**만. 명칭 예: `kc-v09-stage-pilot-<unique-id>`. 실제 ID는 신뢰된 Provider GET에서 고정, 프로젝트 파일에 복사하지 않는다.
- 금지: 기존 MockTimer 계정 전체, Development 계정의 기존 5개 cutover Worker, OAuth/Access/D1/KV/R2/DO/Service Binding, 기존 운영 Secret. [workspace-infra #230](https://github.com/camem2442/workspace-infra/pull/230)의 Writer와 동시 변경 금지.
- 구성: 자산·바인딩·route·cron 없음, `workers_dev=false`; Account/Worker scope 읽기 전용 Token과 stage 쓰기 Token을 분리. 별도의 발급·삭제 권한 가진 Owner가 토큰을 회수.
- Wrangler 바이너리 버전·SHA와 Node 버전·config의 exact path를 Pin. `npx` 자동 설치 금지. `--name`, `--env`와 실제 Cloudflare **script_name** 결속은 API GET으로 확정하기 전까지 `BLOCKED`.
- 시크릿: `KC_V09_SYNTHETIC_ONLY` 같은 **합성값** 하나만, 실제 OpenAI API 키를 사용하지 않는다. 로그·영수증에는 값·길이·재식별 가능한 digest를 쓰지 않는다.

## 효과의 순서와 승인 단위

| Phase | 수행 | 인수/실패 경계 |
|---|---|---|
| 0 READ_ONLY | 계정·Worker 인벤토리, Free/Billing 상태, 현재 cutover Writer, exact scope 조회 | 권한·비용 미확인 → HOLD. available quota는 무료 허가 아님 |
| 1 OWNER_BOOTSTRAP | 일회성 Worker를 no bindings, no routes, `workers_dev=false`로 **최초 1회** 배포 | 이 단계는 실제 deploy effect. Owner 별도 승인 필요; K09-04 `stage` 허가가 대체하지 않음 |
| 2 PRE_OBSERVE | `GET /workers/scripts/{script_name}/deployments`(첫 원소가 활성), `GET /versions` | active deployment ID + traffic weights + versions inventory, script_name/계정 고정 |
| 3 AUTHORIZE_STAGE | workspace-infra exact Goal/Writer Grant, target/operation/TTL 및 현재 active digest 고정 | KeyCLI Plan JSON/같은 UID `KC_AGENT`로 권한을 만들지 못함 |
| 4 STAGE_ONCE | 실제 `wrangler versions secret put`를 오직 1회, Secret stdin | `secret put`(즉시 배포), `versions deploy`, `wrangler deploy` 호출 0회 |
| 5 POST_OBSERVE | 같은 계정/Worker에서 new version ID, tag/binding, 활성 deployment/traffic weights 비교 | Version 생성≠Secret 값 증명; Active 변화/태그 불명/조회 오류는 UNKNOWN_EFFECT |
| 6 SAFE_PROBE | 필요 시 독립 테스트 환경의 무해한 기능 검증 | Version URL은 기존 KV/D1/DO/R2 리소스에 연결될 수 있음. 무바인딩 disposable Worker만 사용 |
| 7 OWNER_CLEANUP | Worker·Secret·임시 Token/권한 및 로컬 합성 값 제거 | 별도 제거 허가; 원본 삭제 전 영수증 보관; 404·토큰 목록 조회로 잔여 확인 |

**주요 제한:** Cloudflare `versions/{version_id}` 응답에서 `workers/tag`/bindings가 문서상 항상 나타나는지는 실제 provider 읽기 전 검증되지 않았다. 해당 메타데이터가 없으면 원격 버전의 operation binding을 주장하지 않고 `UNKNOWN_EFFECT`로 유지한다. Secret GET으로 실제 값을 확인할 수 있다고 가정하지 않는다.

## K09-04B 종료 증거 (최소 1회, 자동 재시도 금지)

1. Before/After 동일 계정·script ID·active deployment ID·버전/traffic weight 스냅샷. 비활성 새 version ID 1개만 추가됨.
2. 원장: `INTENT_WRITTEN`→`DISPATCHED`→ ACK/UNKNOWN, crash-restart 후 동일 Target 다른 operation 거부. 다른 호스트의 충돌 제어는 workspace-infra Writer/Claim이 보장한다.
3. Real Wrangler version, script/environment name mapping, Secret stdin 및 stdout/stderr 무노출, 값 검증 없음 명시.
4. 성공/실패 모두 인증된 원본 Provider 메타데이터 수집; 성공한 stdout만으로 PASS 처리 금지.
5. Cleanup 확인 후에만 `OPERATING_ACCEPTED_DISPOSABLE`로 한정 판정. **운영 Worker에 대한 권한·완료 주장 금지**.

## 비용·재시도 예산

- 승인당 **1 bootstrap deploy + 1 inactive stage + 제한 GET + 1 cleanup**로 고정하고 미확인 429/5xx/timeout은 재전송 금지.
- 일회성 Worker여도 현재 플랜·리소스·비용 메트릭이 확정되지 않으면 $0 보증 금지. 기존 account 총 사용량과 다른 작업의 동시 효과도 고려.
- 금지된 실제 효과: 기존 OAuth app 변경, Access bypass, 다른 계정 transfer, production traffic promotion, 서비스 바인딩 재연결.

## 출처

- [Cloudflare Secrets](https://developers.cloudflare.com/workers/configuration/secrets/)
- [Cloudflare version and deployment CLI](https://developers.cloudflare.com/workers/wrangler/commands/workers/)
- [Cloudflare List Deployments](https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/deployments/methods/list/)
- [Cloudflare List Versions](https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/versions/methods/list/)
- [Cloudflare Get Version](https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/versions/methods/get/)

## #55의 단일 버전 2-GET 입증 추가 조건

1. `GET /accounts/{account_id}/workers/scripts/{script_name}/versions/{version_id}`에서 `secret_text` 바인딩 이름을 수집한다.
2. `GET /accounts/{account_id}/workers/workers/{worker_id}/versions/{version_id}`에서 `annotations["workers/tag"]`를 읽고 **같은 version ID**인지 확인한다. 해당 API의 `worker_id`는 Worker ID 또는 이름을 허용하지만, Wrangler `--name`/`--env`가 계산한 실제 script_name과 동일하다는 가정은 라이브 검증 전 금지.
3. 현재 #55는 Worker 이름을 worker_id 위치에 사용한다. 실제 이름·환경 불일치, Beta API 403, 태그 누락, ID 불일치는 `UNKNOWN_EFFECT` 유지. 재PUT·자동 배포 금지.
4. 승인된 disposable Worker의 첫 Bootstrap 배포와 그 이후 `versions secret put`의 비활성 버전 생성은 **별개 승인**. Active deployment ID·traffic weights가 바뀌면 성공으로 인수하지 않으며 Secret 값을 읽어 볼 수 있다고 주장하지 않는다.

공식: [Script version](https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/versions/methods/get/) · [Worker version](https://developers.cloudflare.com/api/resources/workers/subresources/beta/subresources/workers/subresources/versions/methods/get/).
