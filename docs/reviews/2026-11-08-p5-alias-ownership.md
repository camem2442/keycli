# P5 alias ownership repair

The PR #41 review identified a real mismatch: the digest serialization orders
map keys, while resolver and policy alias lookup stop at the first matching
entry in an unordered Go map. Two canonical owners could therefore share the
same alias (including a canonical-name collision) without a unique decision.

The existing policy preflight now includes `Config.AliasErrors()`. It rejects
cross-owner duplicate aliases, case-insensitive canonical/alias collisions and
empty names. Repetition for the same canonical owner is still accepted. Error
messages are sorted and contain names only. No backend is consulted.

This uses the existing validation path rather than another acceptance layer:
`policy validate` returns exit 1; `policy show` remains diagnostic but omits
`policy_digest`; ordinary commands reject the invalid configuration before
secret resolution or child execution. Valid configurations retain their digest
bytes and lookup behavior. The v1 digest's malformed-input omission condition
therefore includes ambiguous alias ownership as well as malformed policy globs.

Tests cover nine ownership cases, stable diagnostics under map iteration,
rejection of ambiguous digest generation, and four real CLI invalid-config
cases with no child marker. A standalone Linux Go stdlib fixture ran the exact
new alias helper and its two unit tests (nine subcases); it used a minimal Config
type and is not a substitute for the repository's full CI. Repository CI must
validate the integrated production Config, middleware and CLI before merge.

No actual user config, secret, token, backend, installed binary or release pin
was changed. A policy digest remains a configuration fingerprint, not authority.
