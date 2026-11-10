# Delivery Plan

`kc delivery plan --profile NAME [--config PATH] [--json]`은 자격증명 전달 요청의 대상과 범위를 로컬에서 확인한다. 프로필 이름은 정확히 일치해야 한다. 기본 파일은 `~/.config/kc/delivery.toml`이다. 일반 파일에 쓰고 `chmod 600`을 적용한다. 파일 크기 상한은 1 MiB다. `--config`는 Delivery 파일 경로를 지정한다. 기존 `KC_CONFIG`는 KeyCLI alias와 정책 메타데이터에만 사용한다.

```toml
schema_version = 1

[profiles.cloudflare_dev]
provider = "cloudflare-workers"
account_alias = "fixture-account"
worker = "fixture-worker"
environment = "staging"
source = "fixture_source"
destination = "FIXTURE_DESTINATION"
effect = "inactive-version"

[profiles.github_openai]
provider = "github-actions-wif"
repository = "example/fixture"
ref = "refs/heads/main"
workflow_ref = "example/fixture/.github/workflows/probe.yml@refs/heads/main"
environment = "ci"
audience = "https://api.openai.com/v1"
wif_provider_alias = "github-fixture"
service_account_alias = "ci-fixture"

[profiles.oracle_ci]
provider = "oci-instance-wif"
tenancy_alias = "fixture-tenancy"
instance_alias = "fixture-instance"
identity_domain_alias = "fixture-domain"
wif_provider_alias = "oci-fixture"
service_account_alias = "oci-service"
```

파일에는 대상과 alias만 기록한다. Plan은 App이나 Resolver를 초기화하지 않고 설정을 읽는다. 프로필의 모든 필드와 공급자별 범위를 검사하고, Cloudflare 소스는 기존 KeyCLI alias 규칙으로 해석한다. 강제 정책에서 소스가 거부되거나 alias가 모호하면 오류로 종료한다.

JSON의 `v`는 1이다. `plan_id`는 대상, 효과, 정책 메타데이터를 포함한 정규화 출력의 digest다. `static_validation`은 설정 검사 결과이고 `remote_verification`은 `NOT VERIFIED`다. `unverified`에 실제 신원, 공급자 기능, 권한 등 후속 관측이 필요한 항목을 표시한다. `authoritative`, `acceptance_claim`, `authorizes_execution`은 항상 false다.

계획 생성은 exit 0, 사용법 또는 프로필 선택 오류는 exit 64, 설정 읽기·검증 또는 정책 사전 검사 오류는 exit 1이다.

GitHub Actions Secret 전달은 WIF와 별도의 `github-actions-secret` 프로필로 계획한다. 이 경우 예상 효과는 `immediate-secret-update`다.

```toml
[profiles.github_org_secret]
provider = "github-actions-secret"
scope = "organization"
organization = "example"
selected_repository_ids = [19, 2]
source = "fixture_credential"
destination = "FIXTURE_DESTINATION"
effect = "immediate-secret-update"
```

`scope = "repository"`는 `repository = "example/fixture"`를 요구한다. `scope = "environment"`는 같은 저장소에 `environment`를 추가한다. `scope = "organization"`은 조직과 하나 이상의 서로 다른 양의 저장소 ID를 요구한다. 범위를 섞거나 조직 전체를 선택할 수 없다. 계정·저장소·환경의 실제 결속과 Secret 쓰기는 후속 인증·실행 단계에서 검증한다.

## GitHub Actions WIF 합성 claim 검사

`kc auth probe --profile github_openai --offline --claims fixture.json [--config PATH] [--json]`으로 GitHub Actions WIF 프로필의 claim 조건을 확인한다. `fixture.json`은 아래처럼 직접 만든 합성 JSON이며 일반 파일에 저장하고 `chmod 600`을 적용한다.

```json
{
  "iss": "https://token.actions.githubusercontent.com",
  "aud": "https://api.openai.com/v1",
  "repository": "example/fixture",
  "ref": "refs/heads/main",
  "workflow_ref": "example/fixture/.github/workflows/probe.yml@refs/heads/main",
  "environment": "ci",
  "event_name": "workflow_dispatch"
}
```

7개 문자열 claim이 모두 일치하면 exit 0이다. JSON 출력의 `claims_matched`는 true이고 `authentication`은 `NOT VERIFIED`다. fixture가 잘못됐거나 값이 불일치하면 exit 1이며 입력값은 오류에 출력하지 않는다. 이 경로는 합성 claim 비교만 수행하며 서명 검증, 토큰 교환과 사용은 별도 실제 인수 항목이다.
