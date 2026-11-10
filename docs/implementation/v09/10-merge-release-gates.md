---
status: PARTIAL
date: 2026-11-10
owner: keycli
scope: source/CI evidence and prospective release decision
---

# v0.9.x → v0.10.0 — 실제 PR 의존성 및 릴리스 게이트

## 기준 증거 (2026-11-10)

| PR | HEAD | 선행 브랜치 | CI 및 제품 상태 |
|---|---|---|---|
| [#46 설계](https://github.com/camem2442/keycli/pull/46) | `f72d840d` (이 문서 추가 이전) | main | 문서-only, 최근 hosted CI 없음 |
| [#47 K09-01~03](https://github.com/camem2442/keycli/pull/47) | `e61beed` | main | [Linux #42755608356](https://github.com/camem2442/keycli/actions/runs/42755608356) SUCCESS; 실제 WIF 교환 / GitHub Secret 쓰기 NOT VERIFIED |
| [#48 K09-04A](https://github.com/camem2442/keycli/pull/48) | `1edf3cc` | #47 | 단일 Cloudflare version-only adapter, Go 1.23 isolated tests; full Go 1.22 CI NOT VERIFIED |
| [#50 K09-04B](https://github.com/camem2442/keycli/pull/50) | `998ca933` | #48 | journal + GET-only provider observer; 실계정/real Wrangler script+env mapping NOT VERIFIED |
| [#49 K10-01](https://github.com/camem2442/keycli/pull/49) | `ffc83ea` | #47 | 순수 rotation planner, isolated tests; full CI NOT VERIFIED |
| [#51 K10-02](https://github.com/camem2442/keycli/pull/51) | `8dce0c6` | #49 | host-owned preparation + journal, isolated tests; live issuer 없음 |
| [#52 K10-03](https://github.com/camem2442/keycli/pull/52) | `82269ad` | #51 | host-verified consumer evidence, isolated tests; genuine provider evidence 없음 |
| [#53 K10-04](https://github.com/camem2442/keycli/pull/53) | `7e6da52` | #52 | separate revoker grant / one generation, isolated tests; live revoker 없음 |
| [workspace-infra #251](https://github.com/camem2442/workspace-infra/pull/251) | `c221547` | workspace-infra main | 권한/영수증 소유자 연결 **문서-only** |

현 기록은 **코드가 원격 게시된 것**과 **Go1.22 본선에 통합 가능**한 것과 **운영 신뢰·권한을 받은 것**을 구분한다. `[skip ci]` commit에서 workflow run이 없다면 CI=NOT VERIFIED이지 PASS가 아니다.

## 실제 Git 의존성

```text
keycli/main
 ├─ #46 (문서만, #47과 독립)
 └─ #47 K09-01..03 (CI SUCCESS)
     ├─ #48 K09-04A
     │    └─ #50 K09-04B + inactive-version observer
     └─ #49 K10-01
          └─ #51 K10-02
               └─ #52 K10-03
                    └─ #53 K10-04
workspace-infra/main
 └─ #251 KeyCLI consumer mapping docs (실행 adapter 없음)
```

#47 병합 승인 이후 #48/#49의 base를 현행 main으로 바꾸거나, 작업 규칙에 맞춰 충돌 없는 순차 통합을 진행한다. 다른 스택의 HEAD를 강제로 rebase하지 않는다. #50은 #48 뒤에, #51→52→53은 #49 뒤에만 병합한다. 변경된 HEAD/merge ref는 과거 CI 증거와 다른 소스이므로 검증의 최신성도 재평가한다.

## 릴리스 제안 및 Gate

| 후보 | 구성 | Release gate | Operating gate |
|---|---|---|---|
| **v0.9.0** | #47 + v09 문서 참조 | 최종 source Linux Go1.22 whole suite, binary SHA, 격리 Mac 핵심 smoke, rollback 절차 | 실제 GitHub WIF/REST는 별도 provider acceptance. CLI Plan만으로는 Secret 이전 불가 |
| **v0.9.1** | #48 + #50 | 최종 합성 scope, version-only Go 1.22, journal·GET parser negative tests, 클라우드 staging 보호 | 정확한 account·script `--name/--env` mapping + disposable Worker의 실제 inactive version & unchanged traffic |
| **v0.9.2** | WI-KC-01 (workspace-infra) | 기존 Canonical Goal/KeyCLI policy receipt의 독립 read-only projection | authenticated host, authority/operation source binding, no untrusted receipt lift |
| **v0.9.3** | K09-05 OCI WIF (future) | OCI signer/IDCS claim / Go Go1.22 fake + standard-vm adapter gate | 별도 Free+auth/Instance+runner G1~G7, 실계정 NOT VERIFIED |
| **v0.10.0 core** | #49 + #51 + #52 + #53 | 최종 Go1.22 whole suite, true journal FS integration and consumer evidence verification | provider issuer/revoker adapter, real service account scopes, rollback grace, true end-to-end generation rotation 필요 |

v0.9.3 OCI가 완료되지 않았다는 이유로 **소스-only** rotation core 구현을 막지 않는다. 하지만 v0.10.0을 `automatic rotation operating complete`로 소개하려면 각 provider의 비운영 발급·승격·소비자 검증·폐기 실험 및 신뢰 호스트 계약이 있어야 한다. 해당 증거가 없다면 출시 범위를 `experimental API / internal core`로 제한하고 표면 CLI는 기본 비활성으로 둔다.

## CI/비용과 안전 원칙

- #47 Linux Run #42755608356는 **오직 해당 SHA 범위의 CI 증거**다. #48~#53의 추가 파일을 자동으로 검증한 것은 아니다.
- 새 PR은 GitHub-hosted Linux 자동 비용이 발생할 수 있어 `[skip ci]`를 사용했다. 준비 중인 #239 중복 분류가 완전한 비용·권한 게이트라고 가정하지 않는다.
- 격리 Go 1.23/amd64·합성 HTTP·시험용 journal shim 테스트는 구현 후보 자체에 유효한 참고 증거. 실제 KeyCLI Go1.22.12 및 원본 `journal.Store` + protected host E2E는 **반드시 별도** 확인한다.
- CI 최소화: 현행 host에서 읽기 전용 main/head/변경 영향 조회 → local/self-hosted Linux first → 실제 릴리스 candidate HEAD에서 비용 승인된 bounded hosted CI 단발. Skip된 CI를 비용 절감의 PASS로 표시하지 않는다.
- KeyCLI `kc push cloudflare` 기존 경로는 `wrangler secret put` **즉시 배포**. #50의 새 low-level stage는 `versions secret put`만 호출하고 어떤 CLI/운영 Worker도 자동 변환하지 않는다.
- #50의 Cloudflare `--name` + `--env`가 실제 어떤 Worker script로 해석되는지는 미검증. 계정/Worker 환경 식별을 실제 read-only provider 관측과 동일하게 결속한 뒤에야 비운영 live pilot.
- `journal.Store`는 같은 로컬 root의 협력 프로세스만 잠근다. Mac·OCI·Cloud 서로 다른 호스트에 분산 락 없음. 원격 운영 Writer Claim은 **workspace-infra**가 소유한다.
- 실제 Cloudflare version deploy, PR merge, OCI VM start, GitHub Workflow dispatch, service credential revocation은 이 소스 PR 누구에게도 허가되지 않는다. Solo Maintainer의 실제 최종 판단은 기술 검증과 구분한다.

## 다음 최소 진행

1. #50과 #53의 현재 정확한 HEAD에서 Go 1.22 whole module CI 또는 Mac/UTM Linux 검증; fake journal을 실제 원장으로 대체해 오프라인 재확인.
2. 검증 결과가 요구 범위에 맞을 때만 #47 → (#48→#50) 및 (#49→#51→#52→#53)의 순차 통합을 판단.
3. 비운영 provider 파일럿은 별도 승인: Cloudflare disposable Worker, GitHub test repo WIF/Secret, OCI 존재·Free gate. 운영 인수 미확정인 동안 키 회전/폐기 자동화 없음.
