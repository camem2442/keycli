---
status: partial
owner: keycli-transfer
contract_version: 1
---

# Issuer-attested recipient (local candidate)

This opt-in gate checks an independently trusted issuer's signed statement before
opening secret backends or reading the selected secret. It does **not** discover
an issuer, authenticate the current dot runtime by itself, or make a recipient's
self-signed claim trustworthy. Without a real provider trust anchor and an issued
proof, actual environment identity remains **NOT VERIFIED**. The supplied tests
use only synthetic keys and are not live provider evidence.

## Trust and provider prerequisite

The sender must obtain the issuer's Ed25519 public key, stable issuer identifier
and exact environment ID through an independently authenticated provider/admin
channel. That channel must establish what the issuer is entitled to attest. Do
not accept a trust file from the prospective recipient or from the same
unauthenticated channel as its age key. Public key equality, a local environment
label, a GitHub account signature and a user-created signing key do not establish
provider identity. An authenticated admin may operate an issuer only if that
admin independently authenticates the environment and its age-key ownership.

A sender-owned trust JSON file has exactly four fields:

```json
{"version":1,"issuer":"<independently authenticated issuer ID>","public_key":"<standard padded base64 Ed25519 public key>","environment":"<independently confirmed exact environment ID>"}
```

The issuer signing private key stays with the issuer. There is deliberately no
production `sign` command or built-in synthetic issuer. No trust-on-first-use,
embedded certificate/key, remote key discovery, algorithm negotiation, issuer
URL fetch, fallback verification or automatic credential enrollment exists.
Changing/removing trust revokes future checks under that configuration; it does
not revoke an already exported secret. A separate authenticated trust update and
a fresh challenge are required for key rotation. The sender controls the trust
file, config/aliases, clock, HOME and protected parent directories. Current UID
or root compromise, edited/rolled-back replay state and a compromised issuer are
outside this local gate's protection. The issuer must not simply sign arbitrary
caller-supplied recipient keys: its own key-ownership and environment verification
is mandatory and provider-specific, not implemented by this package.

## Sender workflow

1. With the independent trust file already installed, create a public request:

   ```sh
   kc recipient-challenge --attestation-trust /trusted/issuer.json \
     --to "$RECIPIENT" --environment "$ENVIRONMENT_ID" --name demo \
     --output-file /new/request.json
   ```

   This reads configuration/aliases only, not secret values or private age keys,
   and does not start external backends. The recipient must be an age X25519 key.
   The command creates a random 256-bit nonce and saves immutable local
   expectations in `$HOME/.kc/recipient-challenges/<nonce>.json`. The request
   pins the trust key SHA-256, issuer, exact environment, recipient, fixed purpose,
   canonical secret name, issue time and five-minute expiry. Both request files
   are created exclusively with mode 0600. Existing paths are never overwritten.
   State remains if the public output cannot be written; create a fresh challenge
   rather than editing or reusing partial files.

2. Deliver the public request to the independently authenticated issuer using
   its approved mechanism. It reveals the environment ID, age public key and
   secret **name**, never the secret value. Obtain its signed envelope after it
   has verified environment/key ownership. That delivery, any new sharing and
   provider authorization/enrollment require the operator's normal permissions;
   keycli does not perform them or assert they occurred.

3. Export exactly one item with the signed envelope and request nonce:

   ```sh
   kc export demo --to "$RECIPIENT" --output-file /new/bundle.age \
     --recipient-attestation /received/attestation.json \
     --attestation-trust /trusted/issuer.json \
     --attestation-challenge "$NONCE"
   ```

   Any one attestation flag opts into the gate and requires all three nonempty
   values. One explicit X25519 recipient, one item and a new output file are
   mandatory. `--verified-recipient`, `--to-github`, dotenv and `--stdout` cannot
   be combined with attested export. Verification failure never falls back to
   the manual comparison or legacy path. The canonical name/aliases are checked
   again against the saved challenge before any backend is opened or value read.

4. The existing guarded import still requires independently authenticated
   bundle SHA-256, expected name and receiver recipient. Attestation verifies
   the issuer's statement about the recipient, not the bundle's sender or file
   transport. See [transfer contract](transfer.md).

Legacy exports and manual `--verified-recipient` remain opt-in alternatives with
their existing guarantees. This feature is not a mandatory policy barrier:
a caller able to invoke legacy export can bypass it by omitting attestation
flags entirely. Use only attested export where the operational policy requires
issuer verification; enforcement against a malicious sender/operator is outside
this command's threat model.

## Canonical item binding

Before creating a challenge or consuming an attestation, the command validates
the entire in-memory alias namespace. Canonical names and aliases share the same
case-insensitive namespace. Aliases belonging to multiple canonical items,
canonical-name shadowing, case-only canonical collisions and invalid/blank
spellings fail closed. Repeated aliases for the same canonical item are allowed.
Even an otherwise unique requested name is rejected when the configuration has
an ambiguous fallback elsewhere; no backend starts, nonce is consumed or output
is created for this error. The command does not rewrite the configuration.

The command chooses one canonical binding and passes that exact name to proof
verification, value lookup and the bundle key. It does not recanonicalize the
original input alias for each step. The ordinary resolver still performs its
configured canonical/alias fallback, but the validated unique namespace prevents
that lookup or policy canonicalization from selecting another canonical item.
Reads use the canonical item's configured fallback order rather than prioritizing
the caller's original alias spelling. The invocation uses the configuration
already loaded in memory; backend initialization does not reread the config file.
Any future path that mutates that configuration must revalidate this guarantee.
Legacy non-attested alias resolution remains unchanged.

## Wire format and checks

The signed payload is the exact compact UTF-8 JSON object below, with all fields
present, in this order, without whitespace or a trailing newline. Go
`encoding/json.Marshal(Request)` is the canonical encoding, including escaping
of `<`, `>`, `&`, U+2028 and U+2029. Other issuers must produce those exact bytes.
Strings are JSON strings; times are positive integer Unix seconds.

```json
{"version":1,"issuer":"...","key_id":"<lowercase SHA-256 of trusted key bytes>","environment":"...","recipient":"age1...","purpose":"keycli.export.single.v1","secret_name":"demo","nonce":"<32 bytes in unpadded base64url>","issued_at":2000000000,"expires_at":2000000300}
```

The issuer may narrow `issued_at`/`expires_at` within the sender window. Other
fields must match exactly. Ed25519 signs the UTF-8 bytes
`keycli.recipient-attestation.v1`, followed by one NUL byte, followed by the
payload bytes. The received envelope is:

```json
{"payload":"<standard padded base64 payload>","signature":"<standard padded base64 64-byte Ed25519 signature>"}
```

Input/envelope files are bounded to 16 KiB, regular non-symlink files, owned by
root or the current sender UID and not writable by group/others. Unknown,
duplicate, missing, incorrectly cased fields, null objects/fields and trailing
JSON fail. Noncanonical signed payloads and base64 fail. No claims are trusted
before signature verification. Key and algorithm are chosen solely from the
separately supplied trust configuration, never from the envelope.

The sender's system clock must be correct. Future-issued, expired, nonpositive,
backward or longer-than-five-minute windows fail. The local challenge must be
fresh, bound to the current trusted key/issuer/environment and match the command
recipient and canonical item. No clock-skew grace is granted.

After verification, exclusive creation of `<nonce>.used` elects one winner
across threads/processes. File and parent-directory sync must succeed before
backend startup/secret read; errors fail closed and retain the marker. Expiry
is rechecked after persistence. The `.kc` and challenge directory must be owned
by the sender, private and non-symlink directories. Regular-file opens use
no-follow/nonblocking flags to reject links and avoid FIFO hangs. Trusted HOME
and ancestor directories remain an explicit assumption; same-UID directory
swaps/state deletion cannot be prevented by this check.

A successful authorization consumes the challenge even if a later backend read,
output creation or encryption fails. Never delete a used marker to retry. A
fresh request and proof are required. No automatic cleanup is performed. Retain
state and markers through the full validity window and account for backups,
multiple sender homes and clock rollback. Export does not guarantee that the
recipient remains uncompromised after the proof or enforce ongoing secret
expiry, revocation, execution identity or storage protections.

## Verification and remaining acceptance

- Synthetic Go tests cover trusted verification, all claim bindings, untrusted
  signing keys/key substitution, expiry/freshness, strict parsing, one-use
  concurrency across threads and processes, private state and unsafe files.
- CLI regression tests cover challenge creation, negative checks before secret
  resolution/plugin startup, partial opt-in, incompatible manual flags, encrypted
  file output/decryption, replay, ambiguous alias rejection before backend startup/nonce consumption, stable canonical binding and preservation of the latest default-deny policy. Local execution and remaining release/CI scope belong in the candidate report.
- Real issuer integration, authenticated provider administration, live runtime
  identity and real Mac acceptance are **NOT VERIFIED**. Do not use synthetic
  test success as authority to transfer real credentials.
