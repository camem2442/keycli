# Effective KeyCLI policy digest — v1 (P5)

Status: source candidate. No release/host acceptance is implied.

## Producer

`kc policy validate --json` must succeed before an execution receipt is
qualified. `kc policy show --agent AGENT --json` remains schema `v: 1`,
with an additive `policy_digest` field. The field is omitted if *any* policy
glob is malformed, while the diagnostic command itself remains usable.

`policy_digest` is the lowercase SHA-256 of the compact UTF-8
`encoding/json.Marshal` representation of the following Go struct, with
fields in this exact order:

1. `v`: integer `1`;
2. `kind`: string `keycli-effective-agent-policy`;
3. `agent`: effective agent name;
4. `enforced`: whether policy middleware is enabled in the config;
5. `source`: `explicit`, `default` or `none`;
6. `allow`: effective ordered rule list, with absent list normalized to `[]`;
7. `deny`: effective ordered deny list, with absent list normalized to `[]`;
8. `aliases`: the complete canonical-name → ordered alias-list map, with
   absent map normalized to `{}` and absent lists normalized to `[]`.

Go's JSON encoder deterministically orders map keys. Array order remains
significant. The alias table is included because a changed alias may change a
canonical policy decision. No secret backends, tokens, secret values, expiry
records or audit log content are consulted by the calculation.

This is a **configuration identity fingerprint**, not an authorization,
isolation, confidentiality or authenticity claim. A valid digest with
`source: none` or `enforced: false` does not establish access protection.
A same-UID process may also change or bypass local policy.

## Workspace-infra consumer

- Keep the KeyCLI `policy_digest` in a separately typed
  `KeycliPolicyAdvisory` receipt. The existing workspace-infra
  `VerificationPlan.policy_digest` identifies the **verification policy**;
  do not overwrite or equate it with KeyCLI's agent policy fingerprint.
- Bind observed KeyCLI `policy_digest` to an independent expected digest,
  exact repository/PR/HEAD/base, binary SHA, host profile, executing agent,
  bounded credential names, scope, run ID and original source receipt digest.
- Producer and host identity, source receipt digest, policy identity,
  execution/child-start observations and trust enrollment must be checked by
  the owning verifier. A JSON attachment or self-reported hash has no
  independent host authority and must remain `PARTIAL`/`NOT_VERIFIED`.
- Existing Goal gate PASS, deployment, secret push and PR merge authority are
  never inferred from this fingerprint or from credential availability.
- A pre-v1 binary that cannot emit `policy_digest`, including the pinned
  `v0.7.2` pilot release, is **NOT VERIFIED** for this new P5 field.
  Do not silently substitute a main-branch parser or synthesize a missing hash.

## Validation matrix

- Stable for map insertion order and repeated shows.
- Changes when selected agent, enforce flag, deny/allow rules or alias map
  changes; hash excludes secret values.
- Malformed patterns produce no digest; `policy validate` remains a diagnostic.
- Consumer rejects a missing/wrong digest, missing or non-applicable policy,
  malformed identity and unexpected fields.
- Linux source CI is distinct from real Mac, Cursor Cloud, Codex/Claude
  one-turn, GitHub Actions host trust and operational acceptance.
