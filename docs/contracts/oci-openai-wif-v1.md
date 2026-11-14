---
status: partial
owner: delivery
---

# OCI instance principal → OpenAI WIF (v1)

`internal/identity/ociwif` verifies an OCI identity-domain access token issued to
an instance principal and exchanges it for a short-lived OpenAI access token.

This flow uses an **OpenAI Platform API service account**.

## Flow

```
OCI Compute instance (instance principal)
  ├─ IMDS 169.254.169.254 (link-local)              — host signer
  ├─ POST https://<identity-domain>/oauth2/v1/token  — signed with the OCI Go SDK instance principal
  │     grant_type=urn:ietf:params:oauth:grant-type:token-exchange
  │     scope=urn:opc:idm:__myscopes__
  │     requested_token_type=urn:ietf:params:oauth:token-type:access_token
  ▼
OCI access token (subject token, JWT)
  ▼
POST https://auth.openai.com/oauth/token             — OpenAI workload identity federation
  ▼
short-lived OpenAI service-account token
```

The subject token's audience is whatever the identity domain issued. The package
**never rewrites** it to an OpenAI URL, because a rewritten audience would turn a
misconfiguration into an authorization bypass.

## Registered host

`delivery.OpenRegisteredOCIWIF` accepts the admitted host's profile snapshot
and OCI registration. It binds the delivery aliases to the exact OC1 region,
instance and tenancy OCIDs, identity domain ID, IDCS token endpoint, issued
audience, and OpenAI provider and service-account IDs. Keep those values under
host control. Opening the adapter validates configuration. `Get(ctx)` performs
the network requests.

The instance signer uses Oracle's Go SDK with a transport limited to OCI IMDS
v2 on `169.254.169.254` and the registered region's HTTPS federation endpoint.
The signed IDCS request targets the registered HTTPS
`idcs-*.identity.oraclecloud.com` endpoint (optionally region qualified).
The direct OpenAI exchange uses the fixed HTTPS endpoint above. Both routes
disable ambient proxy discovery and redirects. Keep OCI SDK debug logging
disabled on this host. The OpenAI exchange sends the original JWT and
the two mapping IDs in one POST. Failures return redacted errors without a
static credential fallback.

The claim comparison constrains the exchange locally. OpenAI verifies the
original JWT signature and applies the configured provider mapping. Use
`Credential.Authorize` with a client that disables redirects, ambient proxies,
and authorization header logging. The credential stays in process memory and
expires at the earliest validated deadline.

Official contracts checked 2026-11-12

- [OpenAI federation for OCI](https://developers.openai.com/api/docs/guides/workload-identity-federation/oracle-cloud)
- [OpenAI workload identity token exchange](https://developers.openai.com/api/reference/workload-identity-federation)
- [Oracle instance principal SDK](https://github.com/oracle/oci-go-sdk/blob/v65.90.0/common/auth/configuration.go)

## Claims verified

| Claim | Rule | Failure |
|---|---|---|
| `iss` | equals `https://identity.oraclecloud.com/` | `ErrWrongIssuer` |
| `aud` | contains the configured identity-domain URL | `ErrWrongAudience` |
| `sub_type` | equals `instance` | `ErrWrongSubjectType` |
| `ipst_instance` | in the pinned instance list (when used) | `ErrWrongInstance` |
| `ipst_compartment` | in the pinned compartment list (when used) | `ErrWrongCompartment` |
| `domain_id` | equals the pinned domain | `ErrWrongDomain` |
| `ca_ocid` | equals the pinned tenancy | `ErrWrongTenancy` |
| `exp` | after now | `ErrSourceExpired` |
| `exp - iat` | within `MaxSourceLifetime` | `ErrSourceLifetime` |

Response: `access_token` must be non-empty and free of whitespace; expiry must come
from `expires_at`, `expires_in`, or both consistently; it must fall between
`MinTokenLifetime` and `MaxTokenLifetime`. Anything else is `ErrExpiryInconsistency`.

## Verification

`OCI-01` wrong issuer · `OCI-02` wrong audience · `OCI-03` wrong tenancy ·
`OCI-04` wrong instance · `OCI-05` wrong domain/compartment · `OCI-06` expired
source token · `OCI-07` lifetime/expiry inconsistency · `OCI-08` wrong OpenAI
mapping · `OCI-09` endpoint mutation · `OCI-10` no secret leak ·
`OCI-11` exchange timeout · `OCI-12` no automatic retry · `OCI-13` wrong signer
origin · `OCI-14` cross-account mismatch · `OCI-15` successful synthetic exchange,
plus a missing-component/unavailable-signer case.

The host tests also cover profile registration, OCI SDK destination pinning,
the signed IDCS request form, and the OpenAI response and failure contract.
Fixtures use synthetic credentials and claims.

## Operational acceptance

PR-13 covers live instance enrollment, real IDCS claims and OpenAI mapping,
and a limited OpenAI API request. The current tests use synthetic credentials
and transport responses. The adapter supports Compute instance identity in
the commercial OC1 realm.
