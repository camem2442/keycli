# Portable OAuth Go 0.1.0 candidate — NOT PUBLISHED

Module path: `github.com/camem2442/keycli/sdk/portableoauth`.
All runtime source and test files are a byte-identical mirror of the existing `internal/auth/oauth` package, pinned to the KeyCLI #94 candidate HEAD. The nested `go.mod` declares Go 1.22, no external dependencies and no `replace` directives; this candidate can be imported from a Go package outside the `internal/` visibility boundary. `import_test.go` exercises that public module path explicitly.

## Private, isolated VM qualification

The existing KeyCLI no-queue GitHub workflow is disabled deliberately. Do not replace it with a persistent Flip3 runner, paid hosted job or untrusted action. In the **original owner-authorized isolated KeyCLI VM**, after confirming the exact clean PR HEAD and policy:

```sh
python3 -I -B scripts/check_portableoauth_sdk_mirror.py
(cd sdk/portableoauth && GOWORK=off GOPROXY=off GOTOOLCHAIN=local go test -count=1 ./...)
(cd sdk/portableoauth && GOWORK=off GOPROXY=off GOTOOLCHAIN=local go vet ./...)
(cd sdk/portableoauth && GOWORK=off GOPROXY=off GOTOOLCHAIN=local go test -race ./...)
```

The actual owning root KeyCLI suites, original host/VM authority and independent security review remain required, even if these local package tests pass.

## Version/tag and usage

**No tag has been created.** Since this is a module nested at `sdk/portableoauth` within a multi-module repository, the eventual *module-relative* Git version tag is `sdk/portableoauth/v0.1.0`, not a misleading repository-root `v0.1.0`. A real consumer must pin the exact approved revision in its `go.mod` and `go.sum` and pass locked offline tests before a tag is authorized. The current parent KeyCLI `internal` copy remains in place; the source drift check must be repeated at the final merged exact HEAD.

**Security boundary:** `Begin`/ `ParseCallback` are side-effect-free OAuth authorization request/callback parsing. They do not implement PKCE token exchange, external OS browser dispatch, login timeouts, *one-shot* state consumption, service authorization, session storage, refresh or logout. No agent may treat a positive ParseCallback result as permission to execute effects. Passkey private keys remain with the platform authenticator.

Cross-repo contract and release candidate gate: workspace-infra #883 and #884. No Cloudflare/Keychain/Apple account change, provider token, deployed service or published SDK is created by this candidate.
