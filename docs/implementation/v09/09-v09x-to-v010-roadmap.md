---
status: NOT VERIFIED
date: 2026-11-10
scope: proposed-staged-rollout; no operating grant
---

# KeyCLI v0.9.x → v0.10.0 — 실행 가능한 단계·인수·복구 계획

## 근거와 현재 제한

- KeyCLI v0.8.0 = 출시된 안정 버전. main 및 Mac 설치/Cloud Agent Pin은 독립적으로 유지.
- [K09-01~03 Draft #47](https://github.com/camem2442/keycli/pull/47), HEAD `e61beed451a5807023975f8650a57aa525097070`: 메타데이터 Plan, GitHub OIDC→OpenAI WIF exchange, encrypted GitHub Secrets REST, `journal.Store` 및 status CLI. [Linux Actions #42755608356](https://github.com/camem2442/keycli/actions/runs/42755608356) SUCCESS. 실계정 WIF, Actions Secret PUT, host admission NOT VERIFIED.
- [문서 Draft #46](https://github.com/camem2442/keycli/pull/46)과 [workspace-infra Draft #251](https://github.com/camem2442/workspace-infra/pull/251)는 각각 설계·소비자 매핑이지 운영 권한이 아니다.
- K09-04 Cloudflare Stage는 별도 작업, OCI WIF 및 실제 인스턴스는 아직 미인수. Cloudflare 이전 #230과의 동시 Worker 쓰기는 금지.
- [OpenAI 공식 token exchange](https://developers.openai.com/api/reference/workload-identity-federation)의 성공 응답에는 `expires_in`과 `expires_at` **모두 존재**. 둘의 불일치, 외부 JWT `exp`보다 늦은 만료 및 3600초 초과는 거부.
- 로컬 `journal.Store`의 lock은 **같은 root를 쓰는 협력 프로세스** 사이에서만 작동한다. OCI VM·Mac·Cursor Cloud의 분산 lock이 아니다. 원격 Writer는 workspace-infra가 소유.
- `ACKNOWLEDGED`는 Secret 값 일치나 외부 검증 PASS가 아니다. 관측 실패 시 새로운 효과 이전에 읽기 전용 reconcile 필요.

## 정확한 단계와 릴리스 조건

| 구간 | 효과 범위 | 최소 acceptance | 금지되는 승격 |
|---|---|---|---|
| v0.9.0 candidate | #47의 Plan/WIF/GH REST/journal + 회귀 | 같은 final HEAD Linux 전체 suite, Mac 설치 전 dry-run·영수증; 공개 릴리스는 별도 허가 | GitHub CI PASS → live WIF PASS |
| v0.9.1 / K09-04A | Wrangler `versions secret put` 전용 low-level adapter | 값은 stdin, 출력 유출 0, secret/deploy 0, 취소/실패 UNKNOWN | CLI exit 0 → Worker 활성 배포 불변 |
| v0.9.1 / K09-04B | 기존 journal + trusted host observer 결합 | exact account/worker/env, writer grant, before/after active deployment ID & traffic weight 일치, no automatic retry | 로컬 lock → cross-host global lock |
| v0.9.2 / WI-KC-01 | workspace-infra read-only KeyCLI receipt projection | recorded source/head/policy/host/operation + provider 원본, authoritative=false, scope negative regression | receipt hash → 검증 신뢰 |
| v0.9.3 / K09-05 + SV-OCI-02 | OCI Instance Principal → IDCS → OpenAI WIF + standard-vm G1~G7 | offline signer/claim 먼저, 실제 Oracle VM은 별도 Free·인스턴스 허가 및 G4/G6 독립 관측 | API 인증 가능 → VM 생성·runner/dispatch |
| v0.10.0 / K10-01~04 | service credential lifecycle rotation / recovery | 다음 세대 준비·효과 기록·기능 검증·통제된 activation·rollback grace·기존 키 폐기 소유권 | stage 성공 → 이전 키 즉시 폐기 |

첫 v0.9.x 배포에는 KeyCLI 신규 Cloudflare Stage CLI를 일반 에이전트에게 열지 않는다. 허가된 호스트가 internal adapter를 호출하는 구조로 유지한다. v0.10.0에서 권한·공급자별 운영 인수가 완료된 뒤 별도 public UX를 검토한다.

## K09-04A 개발 범위 (즉시 수행 가능한 소스 작업)

- 기본 `internal/target/cloudflare/cloudflare.go`의 `kc push cloudflare`는 `wrangler secret put`을 사용해 **즉시 배포**한다. 기존 경로는 변경/호환을 별도 결정하기 전까지 운영 Worker에서 사용 금지.
- 신규 내부 경로는 `wrangler versions secret put NAME --name WORKER --env ENV --config CONFIG` 하나만 허용한다. 별도 API 인증·value equality·version ID 조회는 **K09-04B**의 호스트/observer가 소유.
- 사전 검사는 정확한 account ID/worker/env/name, pinned executable/정확한 config, 신뢰된 공급자 환경변수. Secrets는 소유한 buffer를 stdin에 주입하고 제거. Ambient env·stdout/stderr·값 포함 errors 금지.
- binary 실행이 시작된 뒤 비정상 종료·타임아웃이면 `UNKNOWN_EFFECT`, 자동 재시도 금지. exit 0은 `ACKNOWLEDGED_UNVERIFIED`.
- 합성 fake Wrangler는 명령 타입, argv, stdin, 환경변수 유출 여부, cancel, symlink, unexpected credential, invalid target을 테스트.
- **현재 독립 합성 구현은 KeyCLI 전체 모듈에 합쳐진 것으로 간주하지 않는다.** 실제 통합 PR에서 source/Go 1.22/gofmt/CI 검증을 다시 거친다.

## K09-04B — Journal/Observer 통합

`workspace-infra`가 유효한 Goal claim/operation grant와 provider exact-target identity를 확인해야 `cloudflaredelivery.Run`으로 들어온다. 재구성한 Plan의 digest, 신뢰 호스트의 grant digest, Secret Source의 policy digest, 계정 ID·Worker·환경·활성 deployment 기대값이 일치해야 한다. 기존 `journal.Store.Dispatch`는 write intent와 dispatch marker를 fsync한다. 서브프로세스 성공 후 remote GET으로 **새 version identity와 active distribution 불변**을 관측할 때만 stage receipt를 기록한다. 원격 변경 결과가 불명확하면 UNKNOWN_EFFECT에서 조치 보류.

Preview/Version URL은 독립적인 실제 데이터 격리 보증이 아니다. disposable Worker와 분리한 리소스를 사용하며, 기존 Development #230/MockTimer 운영 Worker를 시범 대상으로 삼지 않는다.

## v0.10.0 — 회전 기능을 4개 작은 변경으로

- **K10-01 Pure Planner:** `CredentialGeneration{source,provider,target,generation,expiry,consumers}`와 dependency graph. WIF/Job GITHUB_TOKEN 등 단기 자격증명은 일반 장기키 rotation 대상에서 제외. 회전 대상·소유자·TTL·실패 차단을 값 없이 계산.
- **K10-02 Prepared Generation:** 하나의 대상·새 키 세대에 대해 별도 host grant를 받고 새 Credential을 해당 provider staging에 준비. 이전 키는 유지; Cloudflare Versions/Secret, GitHub env secrets 같은 대상별 가용 범위 확인.
- **K10-03 Consumer Attestation:** 실제 독립 host/consumer가 신규 세대로 기능 인증한 사실, source HEAD, 필요 권한, staging→active 시점, rollback grace와 의존 작업 종료를 관측. Secret GET 이름만으로 value match 확정 금지.
- **K10-04 Finalize/Revocation:** 별도 고위험 권한(issuer/revoker)을 가진 Owner/host가 이전 세대 폐기. 요청 만료/unknown consumer/롤백 기간 미경과/타깃 identity 변경/업스트림 IAM 불일치 시 반드시 HOLD. 특정 세대만 폐기, vault/global sweep 금지.

### 테스트·효과 게이트

각 PR은 Linux-first pure/fake suites를 기본으로 하고 Mac native/actual provider 별도 인수. PR에 새 GitHub-hosted CI가 자동 트리거될 수 있으므로 CI 비용/skip 정책을 먼저 확인한다. 새로운 배포/키 발급/Oracle VM/Actions dispatch/PR Merge는 별도 승인 없이는 실행하지 않는다. 개인 Solo Maintainer의 최종 결정은 기술 PASS와 구분하며 다른 사람의 APPROVED 필수 조건을 추가하지 않는다.

**Rollback:** v0.8.0 설치·Cursor Cloud v0.7.2 Pin·이전 token generation을 현행 상태로 보존. Cloudflare Worker Version rollback은 KV/R2/D1 데이터 변경까지 되돌리지 않는다. 키를 먼저 폐기하면 코드 롤백으로 인증이 복구되지 않을 수 있으므로 revoke는 가장 마지막.
