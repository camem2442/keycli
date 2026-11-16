---
status: NOT VERIFIED
scope: proposed-oci-integration
---

# Oracle OCI Instance Principal → OpenAI WIF 및 임시 Runner

## 현재 상태를 과장하지 말 것

[standard-vm PR #7](https://github.com/camem2442/standard-vm/pull/7)는 Oracle ARM64 adapter의 **Draft/source-only** 구현이다. Oracle 로그인/인스턴스 생성/Runner live Job은 PR의 인수 상태와 분리한다. PR 브랜치 문서에는 2026-11-10 Osaka 읽기 전용 조회에서 인스턴스 0·VCN 0·블록 볼륨 0 및 source test 104 PASS가 기록되어 있으나, 이는 OCI Live Runner나 WIF 인증 PASS가 아니다. 실제 provider 값은 작업 시 다시 조회해야 한다.

## 인증 2개를 구분

1. OCI 리소스 API: VM 안의 Instance Principal + OCI IAM 정책. 검증 예: `oci os ns get --auth instance_principal` (VM 외부에서는 동작을 가정하지 않음).
2. OpenAI API: OCI Instance Principal signer가 **동일 테넌시** Identity Domain으로 OAuth token exchange를 서명 → 발급된 IDCS access token → OpenAI API WIF에 전달 → 제한된 OpenAI 서비스 계정의 단기 access token. OpenAI 장기 API 키·OCI 사용자 API private key를 VM 이미지에 복제하지 않는다.

OpenAI WIF와 Codex WIF는 조직·권한 경계가 다르므로 이 문서는 OpenAI Platform API 서비스 계정 경로만 다룬다.

## 실제 인스턴스가 있을 때의 단계 (Owner 승인 후)

1. `standard-vm`/OCI에서 지역·Compartment·인스턴스 OCID·소유한 OS 사용자·Instance Principal 가용성을 **별도 read-only** 검증. Instance metadata(169.254.169.254)는 VM 내부에서만 사용하며 결과 ID를 로그에 유출하지 않는다.
2. 신뢰된 VM에서 OCI SDK `InstancePrincipalsSecurityTokenSigner`로 IDCS token exchange. `scope=urn:opc:idm:__myscopes__`. Identity Domain URL은 해당 테넌시 것으로 고정, 임의 대체 금지.
3. 발급된 실제 IDCS JWT의 `iss` / `aud` / `sub_type` / `ipst_instance` / `domain_id` / `ca_ocid`를 **토큰을 출력하지 않는 신뢰된 프로브**에서 확인. `iss`가 글로벌 `https://identity.oraclecloud.com/`일 수 있고, `aud`는 Oracle 도메인 URI일 수 있다. OpenAI API URL로 audience를 임의 변경하지 않는다.
4. OpenAI Platform WIF Provider: 정확한 `iss` + 발급 토큰 `aud` 중 하나. 필요 시 tenant-specific **Custom OIDC discovery URL** 사용; 미지원이면 Oracle 도메인 공개 JWKS 업로드 방법 검토. issuer와 discovery URL을 동일 문자열로 가정하지 않는다.
5. Service account mapping은 가장 작은 단위인 실제 `ipst_instance` OCID를 우선하고, `domain_id`·`ca_ocid`·`sub_type=instance`를 결속한다. 여러 인스턴스 동시 승인할 이유가 있을 때만 compartment 범위로 확장.
6. 공식 OpenAI SDK OCI WIF token-provider 경로를 사용. 401/403/발급 제한은 FAIL, 임의 static API key fallback 금지.
7. 실호스트와 provider가 서로 맞고 정확한 API 프로젝트에 비용이 기록되는지 별도 소액 파일럿(명시 승인)으로 관측. WIF가 사용 가능하다는 것과 무료 크레딧 사용은 별도 사실이다.

## standard-vm Runner G1~G7 결속

| Gate | 외부 관측 | 권한 경계 |
|---|---|---|
| G1 | 정확한 GitHub Repo / Runner ID / labels / online | Runner 등록 권한 |
| G2 | 실제 workflow checkout SHA = 승인 HEAD | workflow owning runner |
| G3 | 선택 Suite의 진짜 결과 | canonical verification |
| G4 | GitHub 임시 Runner 등록 해제 확인 | GitHub GET |
| G5 | `_work` / credentials 정리 확인 | 실행 VM 내부 |
| G6 | OCI Instance 상태 STOPPED 확인 | OCI GET |
| G7 | 비밀 없는 원본 증거 영속 | trusted host |

**G4와 G6는 별개의 의무**이며 어느 하나가 성공했다고 재사용을 허가하지 않는다. 오류/정리 실패/VM 정지 실패 시 `reuse_blocked`, 미신뢰 PR은 privileged Runner에서 실행하지 않는다. VM/VCN/Key 생성과 워크플로 dispatch는 새로 승인된 `standard-vm` live adapter 권한에만 속한다.

## 무료 사용·비용

OCI 문서상 Always Free A1의 월 1,500 OCPU-hour와 9,000 GB-hour는 연속 사용 시 **약 2 OCPU / 12GB**와 같다. Service quota available이 무료 한도나 실제 생성 가능 재고라는 뜻은 아니다. 홈 리전·부트 볼륨·네트워크·유휴 리소스·out of capacity 예외를 따로 검사하고, 확인되지 않으면 생성하지 않는다. 실제 가용성은 계정과 시점별로 다르다.

OCI Vault는 WIF 불가한 별도 서비스에 필요할 때만 추가한다. 기본 설계에는 중앙 Vault·시크릿 전체 복제·OIDC 대체용 장기 키 보관을 포함하지 않는다. OCI VM 생성 전에 KeyCLI 측은 offline synthetic plan/probe로 개발할 수 있어야 한다.

## 인수 테스트

- OCI 시뮬레이터: signer 실패, audience 다름, instance ID 다름, tenancy/domain mismatch, credential leak 각각 거부.
- VM 비존재 시 `auth probe --offline`은 `NOT VERIFIED`이며 원격 인증 완료를 주장하지 않는다.
- 실제 OCI 인증은 사용자 별도 허가+Free gate+Runner G1~G7이 모두 필요. test result는 host-bound receipt로 보존.
- 임시 Runner의 종료는 WIF token·환경변수·디스크 잔여를 폐기할 때까지 완료가 아니다.

## 공식 출처

- https://developers.openai.com/api/docs/guides/workload-identity-federation/oracle-cloud
- https://docs.oracle.com/en-us/iaas/Content/Identity/Tasks/callingservicesfrominstances.htm
- https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm
