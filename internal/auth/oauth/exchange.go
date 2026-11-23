// RFC 6749 section 4.1.3 token request construction for a native public client.
//
// Pure: this file builds form values and performs no network, clock, or storage access.
// The caller posts them and passes the response to ParseTokenResponse.
package oauth

import (
	"errors"
	"net/url"
	"strings"
)

var (
	// ErrExchange means the exchange request cannot be built from the supplied state.
	ErrExchange = errors.New("oauth: unusable token exchange request")
)

// BuildExchange produces the authorization-code token request.
//
// The Pending value is the one Begin returned, so the verifier, redirect URI and resource
// are the exact values the authorization request used. Requiring them to match is what
// stops a caller from reusing a verifier with a different redirect or audience, which
// providers reject at best and mis-issue at worst.
func BuildExchange(provider Provider, client Client, pending Pending, code string) (url.Values, error) {
	if !secureEndpoint(provider.TokenEndpoint) || len(provider.TokenEndpoint) > 2048 {
		return nil, ErrExchange
	}
	if !validClientID(client.ID) {
		return nil, ErrExchange
	}
	if pending.Issuer == "" || pending.Issuer != provider.Issuer {
		return nil, ErrExchange
	}
	if pending.RedirectURI != client.RedirectURI || !validRedirect(pending.RedirectURI) ||
		len(pending.RedirectURI) > 512 {
		return nil, ErrExchange
	}
	if !validCodeVerifier(pending.CodeVerifier) {
		return nil, ErrExchange
	}
	if !safeToken(code) || len(code) > MaxAuthorizationCodeBytes {
		return nil, ErrExchange
	}
	if pending.Resource != client.Resource {
		return nil, ErrExchange
	}
	if pending.Resource != "" && (!secureEndpoint(pending.Resource) || len(pending.Resource) > 2048) {
		return nil, ErrExchange
	}

	form := url.Values{}
	// RFC 6749 section 4.1.3 fixes these names. client_id is present because this is a
	// public client: there is no secret to authenticate with, which is why PKCE is
	// mandatory in the first place.
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", pending.RedirectURI)
	form.Set("client_id", client.ID)
	form.Set("code_verifier", pending.CodeVerifier)
	if pending.Resource != "" {
		// RFC 8707: the resource indicator must be repeated exactly as sent to the
		// authorization endpoint, or the issued token is for a different audience.
		form.Set("resource", pending.Resource)
	}
	return form, nil
}

// MaxAuthorizationCodeBytes bounds the code. Authorization servers issue short values;
// anything larger is a bug or an attempt to make the form builder allocate for it.
const MaxAuthorizationCodeBytes = 4096

// validClientID mirrors the check Begin applies: no surrounding space, no control
// characters, and a length bound so the value fits comfortably in a form body.
func validClientID(id string) bool {
	if strings.TrimSpace(id) != id || id == "" || len(id) > 256 {
		return false
	}
	return strings.IndexFunc(id, func(r rune) bool { return r < 32 || r == 127 }) < 0
}

// validCodeVerifier applies RFC 7636 section 4.1: 43 to 128 characters from the
// unreserved set. A verifier outside that range is rejected by conforming servers, so
// detecting it here keeps the failure local to the caller's state.
func validCodeVerifier(verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	for i := 0; i < len(verifier); i++ {
		ch := verifier[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
		case ch == '-', ch == '.', ch == '_', ch == '~':
		default:
			return false
		}
	}
	return true
}
