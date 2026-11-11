---
status: partial
owner: delivery
---

# OCI instance principal → OpenAI WIF (v1, source-only)

`internal/identity/ociwif` verifies an OCI identity-domain access token issued to
an instance principal and exchanges it for a short-lived OpenAI access token.

This is **OpenAI Platform API service-account federation**. It is not Codex/ChatGPT
subscription sign-in and does not reuse that credential.

## Flow

```
OCI Compute instance (instance principal)
  ├─ IMDS 169.254.169.254 (link-local)              — host signer
  ├─ POST https://<identity-domain>/oauth2/v1/token  — signed with InstancePrincipalsSecurityTokenSigner
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

## Interface

| Type | Purpose |
|---|---|
| `Signer` | host adapter: IMDS + OCI SDK signer → `SourceToken{Raw, Claims}` |
| `Exchanger` | host adapter: OpenAI token-exchange call → `TokenResponse` |
| `Policy` | pinned expectations: issuer, audience, tenancy, domain, instance/compartment, provider mapping, lifetimes |
| `Credential` | opaque bearer token with `ExpiresAt()` and `Authorize(*http.Request)` |

`Policy.Acquire(ctx, signer, exchanger)` validates claims first, then exchanges
**once**. There is no retry path; an unknown exchange result is `ErrExchangeUnknown`
and must be reconciled by the caller.

`ValidateExchangeEndpoint` accepts only `https://auth.openai.com/oauth/token`.

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

## Tests (16)

`OCI-01` wrong issuer · `OCI-02` wrong audience · `OCI-03` wrong tenancy ·
`OCI-04` wrong instance · `OCI-05` wrong domain/compartment · `OCI-06` expired
source token · `OCI-07` lifetime/expiry inconsistency · `OCI-08` wrong OpenAI
mapping · `OCI-09` endpoint mutation · `OCI-10` no secret leak ·
`OCI-11` exchange timeout · `OCI-12` no automatic retry · `OCI-13` wrong signer
origin · `OCI-14` cross-account mismatch · `OCI-15` successful synthetic exchange,
plus a missing-component/unavailable-signer case.

Fixtures are synthetic. No real JWT, token value or claim set from a live tenancy
appears in the tests or in this document.

## Not implemented / not verified

| Item | State |
|---|---|
| Real IMDS + OCI SDK signer adapter | **NOT IMPLEMENTED** (host responsibility) |
| Real OpenAI exchange adapter | **NOT IMPLEMENTED** (host responsibility) |
| Live OCI instance principal | `NOT_VERIFIED` |
| Live OpenAI WIF provider/service-account mapping | `NOT_VERIFIED` |
| Mid-token rotation/refresh scheduling | out of scope |
| OKE pod-level identity | out of scope (node identity only) |

`standard-vm` #7 owns VM lifecycle. KeyCLI does not create, start, stop or register
anything. A deregistered GitHub runner (G4) and a stopped OCI instance (G6) remain
separate provider facts and must not be conflated.

## Security notes

- The subject token and the access token are never logged: both types implement
  `Format` and print a redaction marker.
- `Authorize` attaches the bearer token to `https://api.openai.com` only, refuses a
  redirect-style host, userinfo, existing `Authorization`, or an expired credential.
- A successful synthetic exchange proves contract shape, not provider truth.
