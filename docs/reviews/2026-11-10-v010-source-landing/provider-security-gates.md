---
status: historical
owner: keycli-delivery
last_verified_commit: 7bbc01d533a0c7ca9eb8ff43cf51878f42289ace
---

# Provider 보안 게이트 (fail-closed) 매핑

후보 Tree `ee735340` 기준. 각 항목은 "거부되어야 하는 상태 → 실제 거부 지점 → 회귀 테스트"
순으로 적었다. 테스트는 모두 **합성 transport**(주입된 `http.RoundTripper` 또는
`net/http` 대체 함수)만 사용하며 실제 도메인 호출은 0회다.

## 1. 필수 15항목

| # | 거부되어야 하는 상태 | 구현 지점 | 회귀 테스트 |
|---|---|---|---|
| 1 | 다른 repository/owner/account/region/target | `internal/rotation/finalize.go` `validOldCredentialIdentity`, `provideridentity/*` ID 정규식, `target/gh/rest.go` scope 검사 | `TestFinalizeRejectsProviderMismatchesBeforeEffect`, `TestGitHubRepositorySecretIdentity`, `TestCloudflareAccountTokenIdentity`, `TestOracleVaultVersionIdentity`, `TestPutSecretRESTFailsBeforeWriteOnWrongIdentity` |
| 2 | 바뀐 HEAD/Plan/Grant/generation | `journal.Intent{TargetDigest,PlanDigest,GrantDigest}`, `plan.ID` 재계산, `generation` 번호 비교 | `TestDispatchRecordsAcknowledgementAndRejectsReplay`, `TestPrepareDeniesForgedPlanBeforeEffects`, `TestFinalizeRejectsForgedGrantBeforeEffect`, `TestGitHubDeleteOnlyGenerationSecretAfterGrant` |
| 3 | 만료·재사용 승인 | `Finalize`의 `time.Now().Before(grant.ExpiresAt)` + `context.WithDeadline`, `Prepare`/`Attest`의 30초 freshness 창 | `TestAttestHoldsDuringGraceAndStaleProof`, `TestMissingOrStaleProofBlocks`, `TestFinalizeReverifiesAndRequiresDistinctGrant` |
| 4 | 동일 Target 동시 Writer | `journal.Store.lock` (`flock LOCK_EX|LOCK_NB`, `.lock` 0600) + `ErrBusy` | `TestCrossProcessLockAndCrash`, `TestJournalProcess` |
| 5 | 승인되지 않은 provider mutation | 모든 mutation 함수가 `authorize(ctx, gate, provider, op, target)`를 **먼저** 통과해야 함; `gate == nil`이면 거부 | `TestVersionStageDeniedBeforeSecretLookup`, `TestCloudflareRevokerOnlyExactTokenID`, `TestGitHubDeleteOnlyGenerationSecretAfterGrant` |
| 6 | 불확실 원격 호출 자동 재시도 | `journal.ErrUnknownEffect` 보존 + `TestUnknownRequiresExactReconciliation`; provider redirect 미추적 | `TestTransportFailureAndRedirectAreUnknown`, `TestPutSecretRESTDoesNotFollowRedirectOrReplayUncertainWrite`, `TestFinalizeUnknownRevokeMustNotAutoRetry` |
| 7 | GitHub Actions Secret 생성/삭제 race·값 검증 불가 | `target/gh/rest.go` 쓰기 경로는 sealed box 암호문만 전송, 응답 identity 재확인 | `TestPutSecretRESTExactScopeAndEncryptedBytes`, `TestVerifyTargetRESTChecksAllPagesWithoutFollowingLink`, `TestAdmissionAndUncertainGitHubWrite` |
| 8 | Cloudflare Free 증거 누락·서명 위조 | `delivery/freegate/gate.go` ed25519 서명 검증 + 계정 ID 고정 + 실 재고 일치 | `TestMissingOrForgedFreeEvidenceBlocksWithoutProviderRead`, `TestOwnerAttestedFreePlanRequiresLiveInventory`, `TestInventoryChangeOrCapacityBlocks` |
| 9 | 토큰 만료 누락 / 5분 미만 / 24시간 초과 | `providerlifecycle/cloudflare.go`: `expiresAt.Before(now+5m) || expiresAt.After(now+24h)` → provider 호출 전 거부 (#63) | `TestCloudflareTokenIssuerHasSingleScopedPolicyAndNoValueLeak`, `TestCloudflareTokenSinkFailureIsNotSafeToRetry` |
| 10 | active deployment·traffic weight 변화 | `version_observe.go`: `validWeights`, deployment ID/버전 집합 비교, tag는 독립 Version API로만 확정 (#55) | `TestVersionObserverRejectsUntrustedScopeAndTraffic`, `TestVersionTagNeedsIndependentWorkerVersionAPIProof`, `TestVersionObserveReadsDeploymentAndVersion` |
| 11 | OCI 이전 Secret Version 신원 불일치 | `provideridentity/oracle.go` (tenancy OCID + Vault Secret OCID + 양의 version), `validOldCredentialIdentity` | `TestExactOldCredentialProviderIdentity`, `TestOracleVaultVersionIdentity`, `TestFinalizeExactProviderRevokers/oracle_vault_secret_version` |
| 12 | OCI 삭제 예약 전 consumer/grace 증거 누락 | `Attest`의 `RetirementCandidate` + `AuthorizesRevocation=false`, `ScheduleOracleOldVersionDeletion`의 24h~30d 창 | `TestAttestRequiresAllIndependentConsumerEvidence`, `TestOutstandingGraceBlocks`, `TestOldConsumerAndRemoteUnknownBlock`, `TestCandidateNeverGrantsRevocation` |
| 13 | Provider receipt를 canonical approval로 승격 | `Receipt`는 journal `Observation`으로만 소비되고 `Dispatch` 성공 조건은 `journal.Acknowledged` + `ProofDigest` 형식 | `TestDispatchRecordsAcknowledgementAndRejectsReplay`, `TestRunUnknownWhenDeploymentChangesOrVersionCannotBeBound` |
| 14 | 모델 입력으로 `HostAuthorize` 진정성 위조 | **부분**: `authorize`는 `nil`·오류·빈 target만 거부하고, 콜백의 신뢰 앵커는 호스트 소유 (KeyCLI 내부에서 증명 불가) | `TestRunDeniesInvalidPlanBeforeProviderOrSecret`, `TestVersionStageNoAmbientCredentials` |
| 15 | GET과 mutation 혼동 | `provideridentity/*` 는 read-only GET 전용 패키지, `providerlifecycle/*` 는 별도 패키지로 분리; reconcile은 별도 read grant 요구 | `TestReaderRejectsSignerURLMutationAndNoAuthentication`, `TestGitHubProviderReadBindsFinalizeWithoutRevokerOnDrift`, `TestReconcileRequiresDistinctCurrentReadGrantAndExactOriginalWrite` |

## 2. 정직한 한계 (PASS로 승격하지 않음)

### 2.1 `HostAuthorize` 신뢰 앵커는 KeyCLI 밖에 있다

```go
func authorize(ctx context.Context, cb HostAuthorize, provider, op, target string) error {
	if cb == nil || ctx.Err() != nil || target == "" {
		return errors.New("trusted effect admission required")
	}
	if err := cb(ctx, provider, op, target); err != nil {
		return errors.New("trusted provider effect rejected")
	}
	return ctx.Err()
}
```

콜백이 `nil`이 아니고 승인을 반환하면 코드는 진행한다. **콜백의 진정성**(누가 이 콜백을
등록했는가, 그 프로세스가 실제로 신뢰 호스트인가)은 KeyCLI 안에서 증명할 수 없다.
따라서 항목 14는 `SOURCE_PARTIAL`이며, `TRUSTED_HOST = SOURCE_PARTIAL`로 기록한다.
이를 닫는 작업은 신규 Goal 엔진을 KeyCLI에 만드는 것이 아니라
**WI-KC-02 (Trusted Host Admission + Writer/Grant validation)** 다.

### 2.2 #61 Tree의 문서 회귀

`docs/guides/credential-rotation-lifecycle.md`에서 #56이 추가한 K10-04R 절이
#57/#61 overlay에서 누락됐다. 코드 영향은 없지만 "CI가 검증한 문서"라는 주장은
그 파일에 한해 성립하지 않는다([git-blob-parity-report.md](git-blob-parity-report.md)).

### 2.3 테스트가 증명하는 것 / 증명하지 않는 것

- 증명: 위 15항목의 **분기 조건**이 합성 입력에서 fail-closed로 동작한다.
- 미증명: 실제 GitHub/Cloudflare/OCI 계정에서의 엔드포인트 계약, 권한 그룹 실재성,
  rate limit, 실제 삭제 전파 지연. 모두 `NOT_VERIFIED`.

## 3. 실제 외부 효과 집계

| 효과 종류 | 이번 작업 실행 횟수 |
|---|---|
| 실제 provider GET (GitHub/Cloudflare/OCI) | **0** |
| 실제 provider mutation (create/delete/stage) | **0** |
| Keychain 쓰기/삭제 | **0** (`internal/backend/...` 실행 전후 `login.keychain-db` mtime 불변) |
| `~/.kc` 변경 | **0** (mtime 2026-11-08 유지) |
| Cloudflare Worker / Version 변경 | **0** |
| OCI VM/Vault 작업 | **0** |
| GitHub Actions workflow dispatch | **0** |
| 원격 push / PR 생성 / merge / tag / release | **0** |

합성 시크릿만 사용했고, 실제 자격증명 값·지문은 이 문서에 포함하지 않았다.
