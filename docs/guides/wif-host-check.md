---
status: partial
owner: delivery
---

# 등록 호스트의 OpenAI WIF 확인

`kc auth wif-check`는 승인된 GitHub Actions 또는 OCI Compute workload가
발급받은 단기 OpenAI 토큰으로 등록된 모델 하나의 메타데이터를 조회한다.
승인된 workload의 실행 설정에 등록 파일의 SHA-256과 검증된 `kc`
바이너리 SHA-256을 고정한다. 등록 파일은 workload 계정이 소유하는
`0600` 일반 파일로, 다른 계정이 쓸 수 없는 경로에 둔다.
등록 해시를 작업 입력에서 새로 계산해 승인값으로 사용하면 파일의
출처를 확인할 수 없다.

```bash
kc auth wif-check --registration /absolute/path/registration.json \
  --registration-sha256 "$APPROVED_WIF_REGISTRATION_SHA256"
```

등록 JSON에는 다음 여섯 필드가 필요하다.

| 필드 | 값 |
| --- | --- |
| `v` | 정수 `1` |
| `provider` | `github-actions-wif` 또는 `oci-instance-wif` |
| `delivery_toml` | [Delivery 프로필 TOML](delivery-plan.md) 전체를 담은 문자열 |
| `model` | 조회가 승인된 OpenAI 모델 ID |
| `approved_until` | 작업 승인 만료의 RFC 3339 시각 |
| `binding` | 아래 공급자별 객체 |

GitHub의 `binding`에는 `Profile`, `Audience`, `Binding`을 넣는다.
`Binding`에는 `ProviderAlias`, `ServiceAccountAlias`, `Mapping`을 넣고,
`Mapping`에는 `ProviderID`, `ServiceAccountID`, 숫자 `RepositoryID`와
`RepositoryOwnerID`, `WorkflowSHA`, `Subject`를 넣는다. 승인된
`workflow_dispatch` job에 `id-token: write`를 부여한다.
[GitHub 호스트 계약](../contracts/github-openai-wif-v1.md)

OCI의 `binding`에는 `Profile`, `TenancyAlias`, `InstanceAlias`,
`IdentityDomainAlias`, `ProviderAlias`, `ServiceAccountAlias`, `Region`,
`IdentityDomainEndpoint`, `Issuer`, `Audience`, `TenancyOCID`,
`InstanceOCID`, `DomainID`, `ProviderID`, `ServiceAccountID`를 넣는다.
등록된 상용 OC1 Compute 인스턴스에서 실행한다.
[OCI 호스트 계약](../contracts/oci-openai-wif-v1.md)

등록 객체에는 토큰이나 API 키를 넣지 않는다. 성공 JSON에는 등록 파일
해시, 공급자, 모델 ID, HTTP 상태, 교환된 자격증명의 만료 시각을 기록한다.
이 결과는 해당 모델의 메타데이터 조회 권한을 확인한다. 모델 추론 권한과
토큰 만료 후 실제 서버의 거부는 별도 운영 검증 항목이다.
[OpenAI 모델 조회 계약](https://developers.openai.com/api/reference/resources/models/methods/retrieve)
