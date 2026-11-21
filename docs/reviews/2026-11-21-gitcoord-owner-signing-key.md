# GitCoord owner attestation signing key — custody record

Date: 2026-11-21 · Owner: camem2442 · Status: **COMPLETE** (key created, used once, stored)

This key signs the owner's Cloudflare Free/cost/migration/rollback
attestation consumed by `workspace-infra/tooling/gitcoord_owner_attestation.py`.
It is **not** an API token, **not** an SSH identity, and it grants no
deploy, route-on or ingest authority: a valid envelope only supplies the
four owner hashes the predeploy gate already expects.

## Where it lives

| Copy | Location | Readable by an agent |
| --- | --- | --- |
| keycli (login keychain) | `kc get gitcoord_owner_ed25519` | yes, through `kc` only |
| File | `~/.config/gitcoord-owner/ed25519.pem` (mode `0600`, single link) | yes, as a file path |
| Public half | `~/.config/gitcoord-owner/ed25519.pub.pem` | yes, not secret |
| Apple Passwords (iCloud, iPhone) | item `GitCoord owner attestation key`, username `owner-camem2442-gitcoord-2026-11`, PEM in the note field | no; the app has no CLI or scripting interface |

`kc ls` shows `gitcoord_owner_ed25519`. The `kc` item's note records the
key id, the public-key path and the fingerprint, so `kc ls --json` is
enough to locate the key without reading the value.

## Identity

- Key id (pinned in every envelope and verifier): `owner-camem2442-gitcoord-2026-11`
- Type: Ed25519 (PKCS8 PEM, `openssl genpkey -algorithm ED25519`)
- Public key: `MCowBQYDK2VwAyEAtQAD/uAwYbgs9wYI0ENx56k2JlbA/huw3zI3rwI0SvM=`
- Public-key SHA-256: `2cf5ae707cd22e5e5f8ff7b67da5406dbf3fa10627a821b1fb183289950f6111`

## iCloud / iPhone copy (2026-11-21)

There is no iCloud web Passwords app (`https://www.icloud.com/passwords/`
404s) and no scripting interface on the macOS app (`sdef` fails with
-192), so the copy was made with the app's own CSV import:

- A one-off CSV (`Title,URL,Username,Password,Notes,OTPAuth`) held the
  key id as username, the public-key fingerprint as password, and the
  PEM in `Notes`. It was mode `0600` and lived only for the import.
- The item was imported through the Passwords UI as
  `GitCoord owner attestation key`. Item count went 157 → 158 and a
  search for `owner-camem2442` returns exactly that one entry.
- The CSV was deleted immediately after the import; no plaintext key
  copy remains outside `kc` and `~/.config/gitcoord-owner/`.

The app auto-locks aggressively (it re-locked between automation steps),
which is why the import needed a Touch ID unlock while the UI was open.

## How it is used

```sh
# sign (private key never enters argv or logs)
python3 -I -B tooling/gitcoord_owner_attestation.py sign \
  --predicate <predicate.json> \
  --key ~/.config/gitcoord-owner/ed25519.pem \
  --key-id owner-camem2442-gitcoord-2026-11 \
  --output <envelope.json>

# verify against a fresh read-only census
python3 -I -B tooling/gitcoord_owner_attestation.py verify \
  --envelope <envelope.json> \
  --public-key ~/.config/gitcoord-owner/ed25519.pub.pem \
  --key-id owner-camem2442-gitcoord-2026-11 \
  --census <census.json>
```

The CLI requires the private key to be a single regular file with no
group or other permission bits, owned by the calling user.

## First real use (2026-11-21)

Envelope for the b398 GitCoord predicate verified `VERIFIED` with the
key id above, and the predeploy gate dropped from 7 to 2 route-OFF
blockers (`NATIVE_WORKERD_SHA256_MISSING`,
`PROVIDER_GET_BILLING_PROFILE_UNVERIFIED`). `authorizes_deploy`,
`authorizes_route_on` and `authorizes_ingest` stayed `false`.

## Rotation and revocation

There is no automatic rotation. Replacing the key means: mint a new
key id, re-sign the current predicate, update the verifier's pinned
`--owner-key-id`/`--owner-public-key`, and retire the old `kc` item.
Do not reuse this key for SSH, API tokens or any other purpose.

## Verified follow-up (2026-11-21, same key)

The same key later authorized the gate run that supplied the native hash.
The CI VM was recreated (`3C107E70-…`), `standard-vm-ci` ran once for
`44901826119`, and `NativeVMOriginalEvidence.prove()` returned
`PASS_EXECUTED_FULL` with artifact digest `sha256:819b28fe…`. The gate CLI
now re-authenticates that digest itself (`--native-vm-proof`,
workspace-infra #843) instead of accepting a file, and the live run reduced
route-OFF to one blocker:

```
route_off: HOLD
route_off_blockers: ["PROVIDER_GET_BILLING_PROFILE_UNVERIFIED"]
authorizes_deploy: false
```

Cloudflare returns error `1324` ("billing profile could not be loaded") for
both the agent and Billing Read tokens, so that blocker is provider-side and
remains.

Local evidence (not committed): `~/Developer/reports/gitcoord-native-vm-20261121/proof.json`
and `gate-verdict.json`. Runner registration receipts:
`~/Developer/_management/workspaces/standard-vm/runner-evidence-20261121/`.

## Second signature: predicate v2 with the native digest (2026-11-21)

The owner approved re-signing so the native digest survives a merge. Predicate
schema moved to `gitcoord-owner-cloudflare-evidence/v2` and now requires a
`native` section: repository, PR number, run id, runner id, head/base SHA and
the provider-computed artifact digest. `owner_evidence()` returns five hashes;
the fifth is the artifact digest itself (`819b28fe…`), so it can be compared
with GitHub after the run window closes.

Signed with the same key id `owner-camem2442-gitcoord-2026-11` and fingerprint
`2cf5ae707cd22e5e5f8ff7b67da5406dbf3fa10627a821b1fb183289950f6111`. The
private key was read from `~/.config/gitcoord-owner/ed25519.pem` (0600) and
never entered argv or logs. Envelope, predicate, statement and census live in
`~/Developer/reports/gitcoord-owner-attestation-v2-20261121/` (0600, not
committed). workspace-infra #847 carries the change.

Verified on `main` with the envelope only (no live PR, no proof pointer):
`route_off_blockers == ["PROVIDER_GET_BILLING_PROFILE_UNVERIFIED"]`,
`authorizes_deploy/route_on/ingest == false`.

Operational note: a v1 envelope is now refused at the predicate-type check
(`OWNER_PREDICATE_TYPE_INVALID`, observed), and a v2 envelope without the
`native` section fails as `OWNER_PREDICATE_SHAPE_INVALID`. Keep the old v1
files as history, but any gate run must use the v2 envelope.

## Third signature: predicate v3 and the billing-profile declaration (2026-11-21)

Cloudflare's `/billing/profile` returns 404 with code `1324` on this account;
`payment-methods`, `subscriptions` and `spend-policies` all return `200` and
are empty. Code 1324 means the billing customer record does not exist (a Free
account that never added a payment method), not a missing scope.

Predicate v3 makes that explicit: `plan.billing_profile` must be `present` or
`absent_by_design`. The owner signed `absent_by_design` with the same key id
and fingerprint, and the gate clears the block only when the live read is
exactly 1324. A 403 or any other code still fails closed, and the declaration
is inside `plan`, so `free_period_sha256` covers it.

Live result with the v3 envelope: `route_off` =
`READY_FOR_SEPARATE_DEPLOY_GRANT`, no route-OFF blockers, `route_on` still
HOLD, `authorizes_*` all false. Evidence stays local (0600) in
`~/Developer/reports/gitcoord-owner-attestation-v3-20261121/`.
