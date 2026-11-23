// Authorization server metadata parsing for a native public client.
//
// Pure and side-effect-free: this file fetches nothing. The caller performs the HTTPS
// request against a URL built by DiscoveryURL and passes the body here. The point of
// separation is that the trust decision — "is this document really about the issuer I
// pinned?" — is testable without a network.
package oauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
)

var (
	// ErrDiscoveryDocument means the bytes are not a usable metadata document.
	ErrDiscoveryDocument = errors.New("oauth: unusable metadata document")
	// ErrDiscoveryIssuer means the document describes a different issuer than the pinned one.
	ErrDiscoveryIssuer = errors.New("oauth: metadata issuer mismatch")
	// ErrDiscoveryEndpoint means an endpoint is missing or not an acceptable HTTPS URL.
	ErrDiscoveryEndpoint = errors.New("oauth: metadata endpoint rejected")
	// ErrDiscoveryPKCE means the server does not advertise the S256 flow this client requires.
	ErrDiscoveryPKCE = errors.New("oauth: server does not support required S256 PKCE")
	// ErrDiscoveryClientAuth means the server does not accept a public client.
	ErrDiscoveryClientAuth = errors.New("oauth: server does not accept public clients")
)

// MaxDiscoveryBytes bounds the document. Metadata is small; anything larger is a
// mistake or an attempt to make the parser work for it.
const MaxDiscoveryBytes = 64 << 10

// Discovery is the subset of RFC 8414 metadata this client relies on.
type Discovery struct {
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RevocationEndpoint    string
	Scopes                []string
}

// Provider converts the metadata into the endpoints Begin needs.
func (d Discovery) Provider() Provider {
	return Provider{
		Issuer:                d.Issuer,
		AuthorizationEndpoint: d.AuthorizationEndpoint,
		TokenEndpoint:         d.TokenEndpoint,
	}
}

// metadataDocument mirrors the wire names. Unknown members are ignored on purpose:
// RFC 8414 defines this as an extensible registry.
type metadataDocument struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RevocationEndpoint                string   `json:"revocation_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
}

// DiscoveryURL builds the well-known location for an issuer, per RFC 8414 section 3.1:
// a path on the issuer is appended after the well-known segment, not before it.
func DiscoveryURL(issuer string) (string, error) {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Opaque != "" {
		return "", ErrDiscoveryIssuer
	}
	if !validIssuer(issuer) {
		return "", ErrDiscoveryIssuer
	}
	base := &url.URL{Scheme: "https", Host: parsed.Host}
	base.Path = "/.well-known/oauth-authorization-server" + strings.TrimSuffix(parsed.EscapedPath(), "/")
	return base.String(), nil
}

// validIssuer accepts an HTTPS identifier with no credentials, query, or fragment.
// RFC 8414 section 3.3 forbids the query and fragment outright; credentials are refused
// because an issuer is an identity, not a login.
func validIssuer(issuer string) bool {
	parsed, err := url.Parse(issuer)
	if err != nil {
		return false
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return false
	}
	return len(issuer) <= 2048
}

// ParseDiscovery validates a metadata document against a pinned issuer.
//
// The caller must already have decided which issuer it trusts — typically a compiled-in
// constant or a verified configuration value. This function refuses a document whose
// issuer differs in any character, because RFC 8414 section 3.3 requires exact equality
// and because normalising it away would let `https://issuer/` impersonate `https://issuer`.
func ParseDiscovery(raw []byte, pinnedIssuer string) (Discovery, error) {
	if !validIssuer(pinnedIssuer) {
		return Discovery{}, ErrDiscoveryIssuer
	}
	if len(raw) == 0 || len(raw) > MaxDiscoveryBytes {
		return Discovery{}, ErrDiscoveryDocument
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return Discovery{}, err
	}

	var document metadataDocument
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return Discovery{}, ErrDiscoveryDocument
	}
	// Decode stops at the end of the first value; anything after it means the bytes are
	// not a single metadata document, even if the first value looked valid.
	if _, err := decoder.Token(); err != io.EOF {
		return Discovery{}, ErrDiscoveryDocument
	}

	if document.Issuer != pinnedIssuer {
		return Discovery{}, ErrDiscoveryIssuer
	}
	if !secureEndpoint(document.AuthorizationEndpoint) || !secureEndpoint(document.TokenEndpoint) {
		return Discovery{}, ErrDiscoveryEndpoint
	}
	if document.AuthorizationEndpoint == document.TokenEndpoint {
		// Not fatal by itself, but a server that publishes the same URL for both is
		// almost always misconfigured; refusing keeps a bad document from looking fine.
		return Discovery{}, ErrDiscoveryEndpoint
	}
	// response_types_supported is REQUIRED by RFC 8414; we only implement the code flow.
	if !contains(document.ResponseTypesSupported, "code") {
		return Discovery{}, ErrDiscoveryEndpoint
	}
	// PKCE is mandatory for a public client here, so the advertisement must say S256.
	// An empty list means "not advertised", which is equally unusable for us.
	if !contains(document.CodeChallengeMethodsSupported, "S256") {
		return Discovery{}, ErrDiscoveryPKCE
	}
	// Only a public client is modelled. If the server lists auth methods and "none" is
	// not among them, this client cannot authenticate at the token endpoint.
	if len(document.TokenEndpointAuthMethodsSupported) > 0 &&
		!containsFold(document.TokenEndpointAuthMethodsSupported, "none") {
		return Discovery{}, ErrDiscoveryClientAuth
	}

	discovery := Discovery{
		Issuer:                document.Issuer,
		AuthorizationEndpoint: document.AuthorizationEndpoint,
		TokenEndpoint:         document.TokenEndpoint,
		Scopes:                document.ScopesSupported,
	}
	if document.RevocationEndpoint != "" {
		if !secureEndpoint(document.RevocationEndpoint) {
			return Discovery{}, ErrDiscoveryEndpoint
		}
		discovery.RevocationEndpoint = document.RevocationEndpoint
	}
	for _, scope := range document.ScopesSupported {
		if !validScopeToken(scope) {
			return Discovery{}, ErrDiscoveryDocument
		}
	}
	return discovery, nil
}

// rejectDuplicateKeys walks the document so a repeated member cannot silently override an
// earlier one. The last value would otherwise win in encoding/json, which is exactly how a
// document could carry a benign `issuer` and a hostile `token_endpoint`.
func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return ErrDiscoveryDocument
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return ErrDiscoveryDocument
				}
				key, ok := keyToken.(string)
				if !ok {
					return ErrDiscoveryDocument
				}
				if _, duplicate := seen[key]; duplicate {
					return ErrDiscoveryDocument
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := decoder.Token() // closing brace
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err := decoder.Token() // closing bracket
			return err
		}
		return ErrDiscoveryDocument
	}
	if err := walk(); err != nil {
		return err
	}
	// walk consumed exactly one value. A second Token() must report EOF; any value means
	// trailing content, and a syntax error means the bytes were already broken.
	switch _, err := decoder.Token(); {
	case err == io.EOF:
		return nil
	case err == nil:
		return ErrDiscoveryDocument
	default:
		return ErrDiscoveryDocument
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
