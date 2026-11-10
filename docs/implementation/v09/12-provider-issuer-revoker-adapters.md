---
status: NOT VERIFIED
date: 2026-11-10
owner: keycli
scope: provider-issuer-consumer-revoker-roadmap
---

# K10 Provider Adapters — GitHub · Cloudflare · Oracle 발급/검증/폐기 계약

## 현행 코드와 책임 경계

- [KeyCLI #49→#51→#52→#53](https://github.com/camem2442/keycli/pull/53): `internal/rotation`의 Pure Plan, Journaled Prepare, Attest, Finalize Go 인터페이스는 존재한다. [통합 #54](https://github.com/camem2442/keycli/pull/54)에서 해당 전체 소스가 Go 1.22.12 `verify.sh --suite all`을 통과했다. **현재 실제 공급자 issuer/revoker는 없다.**
- Prepare/Finalize의 callback은 실제 provider API 권한을 갖지 않는다. 임의로 구성한 `TrustedByHost`·digest·Grant가 권한이 되지 않도록 독립된 workspace-infra 호스트가 적격성을 결정해야 한다.
- 같은 Vault 전체를 순회하거나 모든 환경에 `KC_KEY`를 복제하는 자동 동기화는 비목표.

## 1. GitHub — 정적 시크릿 전달 vs GitHub App 단기 토큰

| 분류 | Issue / Prepare | Consumer proof | Revocation |
|---|---|---|---|
| GitHub Actions Job `GITHUB_TOKEN` | GitHub가 Job별 발행 | Job의 실제 authenticated provider 호출 | Job 종료/만료; KeyCLI 장기 회전 대상 아님 |
| GitHub App installation token | App owner가 서명한 JWT로 `POST /app/installations/{id}/access_tokens`; `repository_ids`·`permissions` 최소 지정 | 실제 repo API 권한 성공, 1시간 이내 만료 | 단기 토큰 수명·별도 provider 계약; v0.10 static-key retire 대상으로 만들지 않음 |
| 사용자 소유 PAT | 사용자가 직접 관리; 자동 PAT 발급을 가정하지 않음 | 제한된 repo·scope의 실제 허가 작업 | 사용자가 provider에서 폐기 확인 |
| Actions repository/environment/org Secret | #47 GitHub REST sealed-box PUT, 별도 이름 `FOO_GEN2` 준비 | 새 Secret이 노출되는 승인된 테스트 Job의 최소 동작 | **GitHub Secret 삭제 ≠ 발급자 API 토큰 폐기**. 양쪽 별도 검증 |

구체적 구현: `internal/target/gh/issuer.go`는 App installation token의 시간 제한·권한·repo ID 프로브 **별도 모듈**. 현재 #47 `rest.go`는 encrypted delivery 전용으로 유지. Secret 원문 GET 불가. 이전 Secret과 새 Secret을 같은 원격 이름으로 덮어쓰면 양 세대 병행·rollback이 불가능할 수 있으므로 처음부터 버전별 별도 이름과 consumer config 전환을 강제한다.

## 2. Cloudflare — Worker Secret과 API Token은 다른 수명

| 분류 | Issue / Prepare | Consumer proof | Revocation |
|---|---|---|---|
| Worker 런타임 Secret | 새 `versions secret put/bulk`로 **비활성 버전 준비** | exact version + active 배포 불변, 별도 기능 probe | 기존 Worker version 및 fallback 영향 확인 후, 별도 버전에서 Secret 제거/전환 |
| Cloudflare Account API Token | `POST /accounts/{account_id}/tokens`, **Account API Tokens Write**를 소유한 별도 issuer | 제한된 Worker/계정의 read-only GET + 하나의 승인된 operation, 실제 권한 세트 확인 | 별도 발급/관리자에 의한 해당 Token ID 삭제/비활성화; 기존 소비자 종료 증거 필수 |

**계획:** `internal/target/cloudflare/token_lifecycle.go`를 read/issue/revoke로 분리. Token `Roll`은 이전 값 즉시 무효화 위험이 있어 기본 금지. 새 Token 준비 후 기존 Token을 유지하고, 모든 등록 consumer가 신세대로 작동하는 것을 증명한 후 기존 토큰의 **정확한 ID 하나만** 폐기. 일회성 작업 Token이 자기 자신을 삭제할 권한이 없을 수 있으므로 별도 privileged issuer/revoker를 운영자 소유로 둔다. Writer/effect Grant와 issuer/revoker Grant 재사용 금지.

## 3. Oracle — Instance Principal 우선, Vault는 필요한 경우만

| 분류 | Issue / Prepare | Consumer proof | Revocation |
|---|---|---|---|
| OCI Instance Principal | VM 신원·dynamic group·IAM으로 OCI SDK/CLI가 토큰 발급 | 같은 인스턴스에서 scope 제한 read-only OCI 호출 | 인스턴스·dynamic group/IAM 권한으로 통제; KeyCLI 장기 키 대상 아님 |
| OCI → OpenAI WIF | OCI Identity Domain 토큰을 OpenAI Project service account에 교환 | actual instance OCID·issuer/audience/mapping 및 짧은 OpenAI API 호출 | JWT는 짧은 수명, 매핑 변경은 별도 운영 정책 |
| OCI Vault static Secret | Vault `UpdateSecret`로 새 version을 `PENDING`으로 준비하는 경로 검토 | 올바른 정확한 Vault/Secret OCID의 pending version + 테스트 consumer | 신세대를 Current로 승격한 뒤 rollback grace 지나 정확한 old version retire |

`standard-vm #7`은 VM/ephemeral runner lifecycle의 정본(G1~G7, G4 GitHub Runner 제거 ≠ G6 VM STOPPED). KeyCLI 신규 Adapter는 OCI VM 생성/기동/네트워크/VCN 변경 권한을 갖지 않는다. OCI Vault는 리소스 비용/권한·free limits·secret version 제약을 확인한 후 별도 선택적 구현으로 다룬다.

## 4. 공통 Host·Adapter 인터페이스 (아직 구현되지 않음)

```go
type ProviderIssuer interface {
    Prepare(ctx context.Context, generation Generation, grant VerifiedIssuerGrant) (OpaqueCredentialHandle, error)
}
type ProviderVerifier interface {
    VerifyConsumer(ctx context.Context, generation Generation, consumer ConsumerID, proof TrustedHostObservation) error
}
type ProviderRevoker interface {
    RevokeExactOld(ctx context.Context, old Generation, handle OpaqueCredentialHandle, grant VerifiedRevocationGrant) (RevocationObservation, error)
}
```

이 코드는 **설계용 인터페이스**이며 KeyCLI 현재 Go 모듈에서 바로 컴파일되는 API가 아니다. 발급기·전달기·소비자 검증기·폐기기는 보안 주체가 다르다. 기존 #51 `Prepare`, #52 `Attest`, #53 `Finalize`, #47 Journal을 재사용하고 두 번째 상태 DB/승인 서버를 만들지 않는다.

### 우선 해결할 설계 gap

- **소스 보정됨, 운영 미검증:** #50 Cloudflare `Reconcile`은 새 Read Grant의 `GrantDigest`와 이전 Write Intent의 `OriginalWriteGrantDigest`를 분리하도록 수정됐다. [통합 #54 Go1.22 CI #42766965145](https://github.com/camem2442/keycli/actions/runs/42766965145)는 두 식별자 불일치/재사용 거부 회귀를 포함해 SUCCESS. 실제 호스트의 독립 Grant 발급·서명 검증·cross-host Writer Claim은 아직 미연결.
- Cloudflare `GET versions/{id}`의 metadata annotations/tag가 실제 공급자에서 안정적으로 나오는지 **미검증**. 응답에 없으면 `UNKNOWN_EFFECT` 유지, tag 검증을 제거하지 않는다.
- #53 `RevocationGrant.OldCredentialHandle`은 실제 provider token ID/Secret version ID와 기존 generation의 관계를 소유자 Host가 **독립적으로** 검증해야 한다. 합성 테스트에서 일치한 값만으로 revoker를 활성화하지 않는다.
- 재전송 미확정/동일 Target 다른 원장 Root/unknown consumers/후퇴한 Claim generation/비용·만료 불명은 모두 HOLD.

## 5. 다음 소형 PR 분리

| PR 후보 | 구현 | 테스트 / 운영 게이트 |
|---|---|---|
| K10-GH | GitHub App token issuer probe + Actions Secret generation mapping | OAuth/JWT 권한 범위 + repo ID/expiry. 실토큰은 approved pilot만 |
| K10-CF | Account API Token create/verify/revoke + separate privileged owner | 합성 HTTP, token identity/race/cancel, Cloudflare read-only Pilot 후 한정 write |
| K10-OCI | Instance Principal/WIF probe + 선택적 OCI Vault pending version | fake signer/region/tenancy/OCID, 비용 및 VM 존재 gate |
| K10-HOST | Existing Goal/Claim host-bound grant and read-reconcile split | wrong issuer/revoker, grant replay, cross-host writer conflict fail-closed |

`v0.10.0`을 '세 공급자에서 자동 키 회전 운영 완료'라 주장하려면 각각의 *실제* 발급·전달·승격·소비자 검증·폐기 증거가 필요하다. 없다면 `experimental internal core` 범위로 릴리스하고 모든 mutation CLI는 기본 비활성.

## 공식 참고

- [GitHub App installation tokens](https://docs.github.com/en/rest/apps/apps)
- [GitHub Actions Secret REST](https://docs.github.com/en/rest/actions/secrets)
- [Cloudflare Account Token Create](https://developers.cloudflare.com/api/resources/accounts/subresources/tokens/methods/create/)
- [OCI Instance Principals](https://docs.oracle.com/en-us/iaas/Content/Identity/Tasks/callingservicesfrominstances.htm)
- [OCI Secret Versions](https://docs.oracle.com/en-us/iaas/Content/secret-management/Tasks/create-secret-version.htm)

## 공급자별 최종 폐기 제약 갱신

- **GitHub:** App installation token은 기본 1시간 유효한 단기 인증이므로 정적 키 회전 대상이 아니다. Actions Secret REST는 값을 다시 읽을 수 없으므로 새/기존 이름 병행·신뢰된 Job 기능 검증 필요. Secret 삭제는 외부 공급자의 원본 API 키 폐기가 아니다. [GitHub Docs](https://docs.github.com/en/rest/actions/secrets).
- **Cloudflare:** Worker Secret의 비활성 버전과 계정 API Token은 별도 수명주기. 토큰 발급 권한/폐기 권한 분리, 이전 토큰 사용 소비자 전부 종료 전 Token Roll/Delete 금지.
- **Oracle:** Instance Principal은 공급자가 인증서를 자동 회전한다. OCI Vault는 Pending→Current 승격이 가능하나, [ScheduleSecretVersionDeletion](https://docs.oracle.com/en-us/iaas/Content/secret-management/Tasks/delete-secret-version.htm) 요청이 수락되면 실제 삭제 예정 시점보다 앞서 그 버전에 대한 접근이 차단될 수 있다. 이 기간을 롤백 유예기간으로 사용하지 말 것.
- **K10-04 P0:** `RevocationGrant.OldCredentialHandle`의 정규식·plan digest 검사는 **실제 공급자의 이전 키 ID임을 증명하지 않는다**. 실전 revoke 전 issuer가 발급한 불변 credential/version ID·tenant/target/generation을 독립 provider 관측·호스트 권한에 결속해야 한다. 불일치·미관측이면 HOLD.

발급·소비자 검증·폐기 각각 별도의 권한을 갖는 호스트 Adapter로 구현하며 중앙 Vault·자동 전역 Sweep은 추가하지 않는다.
