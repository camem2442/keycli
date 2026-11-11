---
status: NOT VERIFIED
owner: keycli
date: 2026-11-10
scope: source-only host callback interfaces
---

# K10-02~04 static service key lifecycle (stacked Draft)

This guide describes source candidates stacked after K10-01 [PR #49](https://github.com/camem2442/keycli/pull/49). The new functions are **trusted-host Go APIs only**. There is **no public `kc rotate` CLI**, no provider key issuance, token revocation, automatic promotion, real WIF migration or live Oracle/Cloudflare/GitHub mutation.

## Stages

- K10-02 `NewPreparationPlan` and `Prepare`: immutable metadata-only source/target generation and required consumer list; host admission callback then a **single** provider-issued/staged generation callback inside existing `journal.Store.Dispatch`. The journal may report `UNKNOWN_EFFECT`; no automatic replay.
- K10-03 `Attest`: independent host verifiers must authenticate original stage/promotion records and *every* mandatory consumer’s timely new-generation evidence. It can return `READY_FOR_SEPARATE_REVOKER`, never revocation authority. No self-reported `TrustedByHost` boolean from JSON can bypass the callback.
- K10-04 `Finalize`: re-runs the above verifiers against current evidence, requires a **new, separately privileged revocation Grant** pinned to the exact evidence digest and exact old provider credential handle, then journals **one** callback invocation. Success is `ACKNOWLEDGED`, not independent proof that the provider actually disabled the credential. Uncertain results stay `UNKNOWN_EFFECT`.

## Security boundaries

1. Static service keys only. GitHub ephemeral job tokens, OIDC and OpenAI WIF short-lived tokens are excluded; identity provider / service account rotation has a different contract.
2. Do not put raw credentials, account vault snapshots, environment dumps or secret-derived hashes in plan, journal, tests, stdout, PR or Goal artifacts.
3. The pure `EvaluateRetirement` candidate in #49 is advisory and user-controlled inputs are untrusted. Real trust requires the owning workspace-infra Goal/Claim and authenticated host result; merely registering a closure callback does not create authority.
4. Exactly one provider target + old generation per revocation request; previous generation must remain valid throughout migration and rollback grace. No global sweep, no `Roll` API by default, no deleting a worker/service account on the basis of name-only metadata.
5. `journal.Store` locks a **single private local directory**, not distributed hosts. The true cross-host Writer Claim, token issuer/revoker scope and duplicate suppression are supplied by workspace-infra/provider adapters.
6. `Finalize` rejects synthetic future clock claims (current wall clock ±30s) and requires fresh consumer observations, exact source/target, no unresolved effect, and high-risk grant binding to current evidence. Neither an app-specific revoker nor actual secret-identity CAS is implemented here.
7. The separate host's `Prepare` and `Finalize` effect callbacks MUST verify provider-specific immutable IDs immediately before issuance/revocation. Without provider verification/receipt, operating acceptance is **NOT VERIFIED**.

## Verify

Local isolated Linux amd64 Go 1.23.2 `go test -race -count=1 ./internal/rotation` passed with compile-time journal package stand-in. Each stack step K10-02 / K10-03 / K10-04 was independently compiled/tested on a cumulative source snapshot. Real KeyCLI Go 1.22.12 module and exact-head GitHub CI **NOT VERIFIED**. Required next gates: full repository source/CI, authenticated Goal host admission, safe disposable provider acceptance, and separate Solo Maintainer merge/release authorization.

## K10-04R: 공급자별 이전 세대 Identity 재검증 (후속 Draft)

이 보강은 K10-04의 기존 `OldCredentialHandle`이 임의 문자열에 불과할 수 있다는 문제를 수정한다. `Finalize`는 이제 **별도 신뢰 호스트의 `VerifyOldCredential` 콜백**과 `RevocationGrant.OldIdentityDigest`를 요구한다. 허가를 받은 뒤에도 실제 이전 자격증명의 Provider / Kind / Account / Resource / Physical ID / Generation / Plan / Proof / Freshness가 일치해야만 `journal.Store.Dispatch`를 호출한다. 임의의 JSON 해시나 검증 콜백 등록 자체는 인증이 아니며, 독립된 공급자 GET/Issuer 원본과 workspace-infra Claim이 필요하다.

| Provider | 처리 가능한 구체적 대상 | 금지된 혼동 |
|---|---|---|
| GitHub | 별도 세대 이름(`*_GEN_<old number>`)을 가진 **Actions Secret**과 불변 repository/owner ID | 원본 외부 API 키 회수, 같은 이름 덮어쓰기, GITHUB_TOKEN/App 단기토큰 회전 |
| Cloudflare | 정확한 account ID와 **Account API Token ID** | Worker 런타임 Secret / Version 전환을 API Token 폐기로 오인 |
| Oracle | 정확한 tenancy OCID + Vault Secret OCID + **양의 Secret Version 번호** | Instance Principal 단기신원, 임의 OCID / 버전 0, Delete 예약을 롤백 유예로 오인 |

GitHub Secret 삭제는 외부 서비스에서 발행한 키의 실제 폐기가 아니므로 Owner가 후속 원본 API 폐기를 별도로 확인한다. Cloudflare는 Token Roll을 기본 사용하지 않으며, OCI Vault의 Secret Version 삭제 예약은 즉시 접근 차단 위험이 있어 마지막 별도 권한 작업으로 수행한다. 모든 Provider 구현은 **실제 원격 쓰기/폐기 없음**의 합성 계약이며, 소유자의 실제 호스트 증거가 없으면 `NOT VERIFIED`다.

회귀 범위: 3 Provider 정상/오류 identity fixture, 공급자·계정·리소스·물리 ID·세대·증거 누락/만료 거부, 새 증거 hash mismatch, 공급자 읽기 실패, 실제 Revoker 호출 0회 보장, 미확실 응답 `UNKNOWN_EFFECT` 보존. Go 1.23.2 임시 Journal 대체물에서 테스트/race PASS; **실제 Go 1.22 KeyCLI 원장 통합은 별도 검증 대상**.
