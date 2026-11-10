---
status: NOT VERIFIED
scope: proposed-cli-and-go
---

# v0.9 CLI / 설정 / Go 패키지 계약

## K09-01 소스 후보와 후속 CLI 제안

K09-01의 오프라인 Plan 진입점은 다음과 같다. `--profile`은 필수이며 TOML 테이블 이름을 정확히 선택한다. `--config`를 생략하면 `~/.config/kc/delivery.toml`을 읽는다.

```text
kc delivery plan --profile cloudflare_dev [--config DELIVERY_TOML] [--json]
```

아래 명령은 후속 제안이다.

```text
kc delivery stage --profile cloudflare_dev --plan-id ID --authorization-file HOST_OWNED
kc delivery verify --operation-id ID [--remote-read]
kc delivery status --operation-id ID [--json]
kc delivery reconcile --operation-id ID [--remote-read]
kc rotate plan --credential openai_worker --profile cloudflare_dev
kc rotate prepare --rotation-id ID --authorization-file HOST_OWNED
kc rotate finalize --rotation-id ID --authorization-file HOST_OWNED
kc auth probe --profile github_openai [--offline]
kc auth probe --profile oracle_ci [--offline]
```

`plan`은 전달 설정과 KeyCLI의 유효 정책·별칭 메타데이터를 읽는다. Resolver 초기화·Get/List, Keychain, 플러그인, 비밀값 읽기, 원격 인증·호출을 실행하지 않는다. Plan ID는 시간·로컬 경로·원격 관측을 제외한 버전 고정 메타데이터에 결속된다. 정적 검증이 완료되어도 원격 대상·기능·실행 권한은 `NOT VERIFIED`이며 `authoritative`, `acceptance_claim`, `authorizes_execution`은 모두 false다. Cloudflare 활성 배포와 최신 업로드 버전은 이 단계의 미확인 항목이며 실제 기준값과 실행 권한은 이후 호스트 Admission과 원격 관측에서만 확보한다.

`--remote-read`는 명시적 읽기 단계이며 대상에 결속된 연결·인증이 필요하다. 비밀값이 필요한 쓰기만 승인된 호스트에서 최소 범위로 해석한다. GitHub Actions Secret은 지정 scope에 즉시 반영되며 Cloudflare는 비활성 버전을 한 번 생성한다. GitHub/OCI WIF는 인증 교환을 probe하며 Secret 쓰기 단계가 없다. LIVE 인증·Stage는 workspace-infra의 신뢰 호스트가 대상·Plan·Grant·예산을 결속한 뒤 실행한다. `authorization-file`은 신뢰 호스트의 검증을 거치는 핸들이며 호출자가 작성한 파일로 권한을 부여할 수 없다.

기존 `kc push`는 호환성을 유지하되, Cloudflare 즉시 배포 위험을 분명히 표시하고 **운영 Worker에서 별도 강제 승인 없이는 거부**하는 전환안을 제안한다. 변경은 기존 CLI 계약·테스트와 함께 버전별로 리뷰한다. `kc exec` / `get` / `ls`의 종료 코드·출력 의미를 바꾸지 않는다.

## K09-01 전달 프로필 스키마

```toml
# ~/.config/kc/delivery.toml ; no credentials or raw private IDs in tracked repo
schema_version = 1

[profiles.github_openai]
provider = "github-actions-wif"
repository = "camem2442/workspace-infra"
ref = "refs/heads/main"
workflow_ref = "camem2442/workspace-infra/.github/workflows/ai-eval.yml@refs/heads/main"
environment = "development"
audience = "https://api.openai.com/v1"
wif_provider_alias = "github-main"
service_account_alias = "ci-eval"

[profiles.cloudflare_dev]
provider = "cloudflare-workers"
account_alias = "development"
worker = "keycli-fixture-worker"
source = "openai_worker"
destination = "OPENAI_API_KEY"
effect = "inactive-version"
environment = "staging"

[profiles.oracle_ci]
provider = "oci-instance-wif"
tenancy_alias = "oracle-personal"
instance_alias = "oracle-a1-ci"
identity_domain_alias = "oracle-personal-idcs"
wif_provider_alias = "oracle-ci-wif"
service_account_alias = "oracle-ci"
```

스키마는 중복·모호한 프로필, 미지원 공급자·효과, 누락 필드와 해당 공급자 범위 밖의 필드를 거부한다. GitHub의 `audience`는 명시 입력이며 위 값은 합성 예제다. OCI는 `wif_provider_alias`를 요구하며 실제 issuer와 audience는 신뢰된 VM에서 확인한다. CLI는 1 MiB 이하의 일반 파일을 읽고 그룹이나 다른 사용자에게 권한이 열려 있으면 거부한다. 로컬 설정은 `chmod 600`으로 보관한다. 강제 KeyCLI 정책이 Cloudflare의 canonical source를 거부하면 Plan도 오류로 끝난다. 실제 계정·리소스 ID와 별칭의 결속은 인증된 공급자 조회에서 확인한다.

Cloudflare의 원본 `source`와 원격 `destination`은 분리한다. GitHub의 repository·environment·organization Secret 쓰기는 K09-02 제안이며 각 scope의 검증과 REST 전달 절차는 별도로 정의한다. Cursor runtime Secret·Cloudflare Worker·Actions는 각자의 매핑 정책을 갖는다. [GitHub 전달 계약](03-github-wif.md)

## Go 디렉터리 제안

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
  gh/                   scope-bound GitHub encrypted REST delivery
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

K09-01은 `ParseProfiles([]byte) (Profiles, error)`, `NewMetadataSnapshot(config.Config, agent) (MetadataSnapshot, error)`, `BuildPlan(Profiles, name, MetadataSnapshot) (DeliveryPlan, error)`로 구성한다. 소스 후보 `bcbf5a5`의 `internal/delivery/plan.go`가 JSON v1 직렬화를 정의한다. 아래 타입은 후속 실행 경계의 개념형 인터페이스다.

```go
type SecretRef struct { Name string }
type TargetRef struct {
    Provider, Account, Resource, Environment, Destination string
}
type DeliveryPlan struct {
    SchemaVersion int
    RequestID string
    Source *SecretRef
    Target TargetRef
    KeyCLIPolicyDigest string
    Authoritative bool // always false
}
type AuthorizedOperation struct {
    Plan DeliveryPlan
    Grant HostGrant
    Baseline RemoteObservation
}
type TargetAdapter interface {
    Prepare(context.Context, AuthorizedOperation, SecretBytes) (EffectResult, error)
    Observe(context.Context, TargetRef, string) (RemoteObservation, error)
}
type HostAuthorizer interface {
    Verify(context.Context, DeliveryPlan, HostGrant) (AuthorizedOperation, error)
}
```

`SecretBytes`는 로깅 금지, 장기 유지 금지. `HostGrant`는 CLI 인자/임의 JSON이 아니라 신뢰된 호스트에서 검증한 작업 권한을 표현한다. `AuthorizedOperation`의 원격 기준은 승인된 호스트에서 pre-observe 후 채운다. adapter의 `Prepare`는 절대 Cloudflare `versions deploy`나 GitHub Actions dispatch를 호출하지 않는다.

## 엄격한 operation journal

쓰기 전 `INTENT_WRITTEN`을 기록하고 파일과 부모 디렉터리를 fsync한다. 원격 호출, 조회, 종료 상태를 순서대로 durable 기록하고 원장 권한 `0600`과 대상별 잠금을 사용한다. 공급자가 제공하는 동시성 보장은 adapter별로 검증한다. 원격 결과를 로컬에 기록하지 못하면 `UNKNOWN_EFFECT`로 두고 조회·재조정한다. 다중 대상 작업은 개별 결과에 따라 `PARTIAL` 또는 `UNKNOWN_EFFECT`를 유지한다. 공개 영수증에는 전송값·원문 HMAC·토큰·키 길이를 넣지 않는다.

## 배포 및 호환성

- v0.8.0의 `kc push` 관련 출력/파일 규격을 바꿀 때는 새로운 명령에 기능을 먼저 도입하고 breaking change를 별도 승인한다.
- `kc policy`를 수정하지 않고도 테스트 가능한 순수 Plan 모듈부터 구현한다.
- 전송 전에 용량·과금·실행 범위 확인. 그다음 하나의 synthetic target 파일럿. 전체 Vault 일괄 전송 없음.
