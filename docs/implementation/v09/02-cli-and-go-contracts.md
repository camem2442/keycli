---
status: NOT VERIFIED
scope: proposed-cli-and-go
---

# v0.9 CLI / 설정 / Go 패키지 계약

## 제안 CLI (아직 구현되지 않음)

```text
kc delivery plan --profile cloudflare-dev [--json]
kc delivery stage --profile cloudflare-dev --plan-id ID --authorization-file HOST_OWNED
kc delivery verify --operation-id ID [--remote-read]
kc delivery status --operation-id ID [--json]
kc delivery reconcile --operation-id ID [--remote-read]
kc rotate plan --credential openai_worker --profile cloudflare-dev
kc rotate prepare --rotation-id ID --authorization-file HOST_OWNED
kc rotate finalize --rotation-id ID --authorization-file HOST_OWNED
kc auth probe --profile github-openai [--offline]
kc auth probe --profile oracle-openai [--offline]
```

`plan`은 설정만 읽고, **Resolver.Get/List, Keychain, 플러그인, 실제 Secret, 원격 인증/쓰기**를 호출하지 않는다. 응답은 구체적인 대상·정책·기능 지원·미확인 상태만 표시한다. 확인 불가를 성공으로 출력하지 않는다. `--remote-read`는 별도 명시적 읽기 전용 단계이며 연결·인증이 필요하다. `stage`는 승인된 호스트에서 비밀값을 요청한 뒤 플랫폼별 adapter를 호출한다. 사람의 문자열 입력으로 임의 `authorization-file`을 만들 수 있어도 권한이 생기지 않는다. 실제 호스트 측 auth verifier가 그 파일/핸들을 서명 또는 불변 저장소와 결속하는 구현 전에는 운영에서 Stage 자체를 막는다.

기존 `kc push`는 호환성을 유지하되, Cloudflare 즉시 배포 위험을 분명히 표시하고 **운영 Worker에서 별도 강제 승인 없이는 거부**하는 전환안을 제안한다. 변경은 기존 CLI 계약·테스트와 함께 버전별로 리뷰한다. `kc exec` / `get` / `ls`의 종료 코드·출력 의미를 바꾸지 않는다.

## 제안 설정 스키마 (정본 config.toml과 별개)

```toml
# ~/.config/kc/delivery.toml ; no credentials or raw private IDs in tracked repo
schema_version = 1

[profiles.github_openai]
provider = "github-actions-wif"
repository = "camem2442/workspace-infra"
ref = "refs/heads/main"
workflow_ref = "camem2442/workspace-infra/.github/workflows/ai-eval.yml@refs/heads/main"
environment = "development"
wif_provider_alias = "github-main"
service_account_alias = "ci-eval"

[profiles.cloudflare_dev]
provider = "cloudflare-workers"
account_alias = "development"
worker = "ios-control-plane-staging"
source = "openai_worker"
destination = "OPENAI_API_KEY"
effect = "inactive-version"
environment = "staging"

[profiles.oracle_ci]
provider = "oci-instance-wif"
tenancy_alias = "oracle-personal"
instance_alias = "oracle-a1-ci"
identity_domain_alias = "oracle-personal-idcs"
service_account_alias = "oracle-ci"
```

구현 시 스키마를 엄격히 검증한다: 중복 프로필, 모호한 alias, 지원하지 않는 platform/effect, 비어 있는 scope, 내부 수집용 ID 누락, 환경/계정 혼동은 fail-closed. 사람이 소유한 로컬 config는 `0600`, repo에는 **합성 예제**만. ID·OCID·account 별칭은 실제 authenticated endpoint 조회 결과에 결속되어야 하며 문자열만 같다는 이유로 신뢰하지 않는다.

원본 `source`와 원격 `destination`은 분리한다. 특히 GitHub `--repo OWNER/REPO`는 단일 저장소, `--org ORG --repos ...`는 조직 Secret에 접근할 저장소 집합이라는 차이를 반드시 지킨다. Cursor runtime Secret·Cloudflare Worker·Actions는 서로 다른 매핑 정책을 갖는다.

## Go 디렉터리 제안 (구현 전)

```text
internal/delivery/
  plan.go              pure plan + strict target/identity validation
  profiles.go          config schema v1 loader; no secret resolver construction
  authorization.go     trusted host authorization verifier interface
  operation.go         immutable operation/target identity
  journal.go           fsync + atomic rename + single-writer lock
  receipt.go           non-authoritative metadata receipt
  reconcile.go         unknown effect -> provider remote observation
internal/target/
  cloudflare/           version-only create + GET deployments; no deploy
  gh/                   repo-scoped GitHub secret semantics
  cursor/               scoped runtime secret integration
internal/identity/
  githubwif/            claim-contract probe; no token persistence
  ociwif/               Instance Principal/IDCS claim probe
internal/rotation/
  graph.go              dependency-aware, no global vault sweep
  lifecycle.go          preparation, verification, cleanup hold
internal/cli/
  delivery.go           new Cobra group
  rotate.go
  auth_probe.go
```

권장 Go 개념형 인터페이스(컴파일 가능한 구현이 아님):

```go
type SecretRef struct { Name string }
type TargetRef struct {
    Provider, Account, Resource, Environment, Destination string
}
type DeliveryPlan struct {
    SchemaVersion int
    RequestID string
    Source SecretRef
    Target TargetRef
    ExpectedActiveVersion string
    PolicyDigest string
    Authoritative bool // always false
}
type TargetAdapter interface {
    Prepare(context.Context, AuthorizedOperation, SecretBytes) (EffectResult, error)
    Observe(context.Context, TargetRef, string) (RemoteObservation, error)
}
type HostAuthorizer interface {
    Verify(context.Context, DeliveryPlan, HostGrant) (AuthorizedOperation, error)
}
```

`SecretBytes`는 로깅 금지, 장기 유지 금지. `HostGrant`는 CLI 인자/임의 JSON이 아니라 신뢰된 호스트에서 검증한 작업 권한을 표현한다. adapter의 `Prepare`는 절대 Cloudflare `versions deploy`나 GitHub Actions dispatch를 호출하지 않는다.

## 엄격한 operation journal

쓰기 전 `PREPARED` intent를 fsync하고 원격 호출, 조회, 종료 상태를 순서대로 durable 기록. 저장은 권한 `0600`, 대상·작업별 idempotency/lock; CAS는 현재 provider 배포/인스턴스 상태와 정합해야 한다. 원격 쓰기 성공 후 로컬 저장 실패 시 성공 반환 금지 → `UNKNOWN_EFFECT`와 조회/재조정. 부분 다중 대상 쓰기에는 `PARTIAL`/`UNKNOWN_EFFECT`를 남기며 원자적 전체 성공을 주장하지 않는다. 전송값·원문 HMAC·토큰·키 길이 등은 공개 영수증에 기록하지 않는다.

## 배포 및 호환성

- v0.8.0의 `kc push` 관련 출력/파일 규격을 바꿀 때는 새로운 명령에 기능을 먼저 도입하고 breaking change를 별도 승인한다.
- `kc policy`를 수정하지 않고도 테스트 가능한 순수 Plan 모듈부터 구현한다.
- 전송 전에 용량·과금·실행 범위 확인. 그다음 하나의 synthetic target 파일럿. 전체 Vault 일괄 전송 없음.
