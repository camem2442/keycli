---
status: source-only-pilot
date: 2026-11-21
---

# Portable native OAuth core R0

Package: internal/auth/oauth

Scope: **pure, side-effect-free RFC 8252 / RFC 7636 / RFC 8707 preparation**. No command is installed, no network/browser calls, no token exchange or refresh, no passkey private-key access, no shared session daemon, and no behavioral changes to existing credential backends. Browser owner-session automation PR #82/#84/#87 is separate.

- Provider endpoints are trusted caller-supplied metadata and must be obtained through an issuer-pinned RFC 8414 discovery adapter before use. This core only demands HTTPS endpoints and does not claim it has performed discovery.
- Begin supplies 32-byte independent PKCE verifier and state, code challenge S256, optional pinned resource and scopes, and validated registered callback URI.
- ParseCallback rejects alien redirect URI, parameters other than code/state/optional iss, duplicate params, missing or incorrect state, error response and fragment. Caller MUST keep Pending private, **consume exactly once**, enforce a bounded login window, and perform the actual token exchange with the same code_verifier, redirect_uri, client ID and resource.
- For Cloudflare Managed OAuth, **require** a nonempty RFC 8707 resource at the provider adapter and follow returned OAuth discovery endpoints. Access tokens are opaque; the downstream origin verifies its Access-signed application JWT independently.
- Store actual access and refresh tokens in per-app protected backend (keycli Mac Keychain or Linux age backend) only after a later source/test gate; never log or forward tokens to an agent or stdout.
- A service token/WIF represents an unattended workload and cannot bypass the original owner Goal/Grant authority.

Local isolated Go 1.23.2/Linux validation of these source files: GOPROXY=off go test ./internal/auth/oauth -count=1 -v (4 tests PASS), go test -race ./internal/auth/oauth (PASS), go vet ./internal/auth/oauth (PASS). This is a *standalone module mirror*, not the current keycli repository full gate, native Mac test, or online login acceptance. Remain DRAFT until owning HEAD verifier and independent review.

References: [RFC 8252](https://www.rfc-editor.org/rfc/rfc8252), [RFC 7636](https://www.rfc-editor.org/rfc/rfc7636), [RFC 8707](https://www.rfc-editor.org/rfc/rfc8707), [Cloudflare Managed OAuth](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/http-apps/managed-oauth/).
