# KeyCLI R0: original Goal authority through the actual HostBridge

Status: SOURCE_ONLY / HOST_E2E_NOT_RUN. 2026-11-13.

This adds a separate **dev-only** kc-gateway-r0 executable. It links the real
hostbridge.Bridge/GatewayBinding to an original workspace-infra Goal evaluator
rather than using kc-gateway's permissive protocol-only allowAdmission.

No Cloudflare, GitHub or OCI provider adapter exists in this executable.
Only the exact synthetic/record-local-test operation is classified and
the provider is an exclusive-create local ledger, not an external call.
The static operation class is NEVER an operating provider grant.

Host-owned bindings are supplied as exact source SHA, original Goal STATE,
plan file, Grant SHA, target digest, Writer generation, session and mutation.
The caller cannot supply its own check decision. Each authorisation and the
final effect boundary calls the original Python source checker, which reads
the actual Registry, original Grant, SQLite Claim/Mutation and live lease.
The Go process grants no authority itself. Same-UID isolation remains
unverified; the enclosing Linux VM's owner admission is still required.

The existing shipped kc binary and cmd/kc-gateway S3 test harness remain
unchanged. No Keychain access, live provider, new registration, paid fallback,
merge, deployment or provider unlock is part of this change.

R0 target binary: go build -trimpath -o kc-gateway-r0 ./cmd/kc-gateway-r0
Owned Python checker: workspace-infra tooling/ha_keycli_goal_hostcheck.py
This source candidate cannot be marked PASS until its exact HEAD Go suite
and a trusted Linux VM native->Gateway->Journal receipt are observed.
