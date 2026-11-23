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

// maxEndpointLength matches the bound Begin applies to the same URLs.
const maxEndpointLength = 2048

// Member names are matched exactly. A differently-cased member is treated as absent
// rather than folded onto its lower-case twin: encoding/json's own field matching folds
// case and some Unicode forms, which is precisely how a document could carry a benign
// "token_endpoint" and a hostile "Token_Endpoint" and have the hostile one win.
const (
	memberIssuer           = "issuer"
	memberAuthorization    = "authorization_endpoint"
	memberToken            = "token_endpoint"
	memberRevocation       = "revocation_endpoint"
	memberResponseTypes    = "response_types_supported"
	memberPKCE             = "code_challenge_methods_supported"
	memberTokenAuthMethods = "token_endpoint_auth_methods_supported"
	memberScopes           = "scopes_supported"
)

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

// DiscoveryURL builds the well-known location for an issuer, per RFC 8414 section 3.1:
// a path on the issuer is appended after the well-known segment, not before it.
func DiscoveryURL(issuer string) (string, error) {
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Opaque != "" || !validIssuer(issuer) {
		return "", ErrDiscoveryIssuer
	}
	// Path is decoded and RawPath the escaped form. Supplying both keeps URL.String from
	// double-encoding (%20 must stay %20) while preserving an encoded separator: an issuer
	// path of /a%2Fb is one segment, not two, and naively decoding Path would split it.
	const prefix = "/.well-known/oauth-authorization-server"
	base := &url.URL{Scheme: "https", Host: parsed.Host}
	base.Path = prefix + strings.TrimSuffix(parsed.Path, "/")
	base.RawPath = prefix + strings.TrimSuffix(parsed.EscapedPath(), "/")
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

	members, err := documentMembers(raw)
	if err != nil {
		return Discovery{}, err
	}

	issuer, err := stringMember(members, memberIssuer)
	if err != nil || issuer != pinnedIssuer {
		if err != nil {
			return Discovery{}, err
		}
		return Discovery{}, ErrDiscoveryIssuer
	}
	authorization, err := stringMember(members, memberAuthorization)
	if err != nil {
		return Discovery{}, err
	}
	token, err := stringMember(members, memberToken)
	if err != nil {
		return Discovery{}, err
	}
	if !acceptableEndpoint(authorization) || !acceptableEndpoint(token) {
		return Discovery{}, ErrDiscoveryEndpoint
	}
	if authorization == token {
		// Policy, not an RFC 8414 requirement: a server publishing one URL for both
		// flows is misconfigured often enough that accepting it would hide the mistake.
		return Discovery{}, ErrDiscoveryEndpoint
	}
	// response_types_supported is REQUIRED by RFC 8414; we only implement the code flow.
	responseTypes, err := stringsMember(members, memberResponseTypes)
	if err != nil {
		return Discovery{}, err
	}
	if !contains(responseTypes, "code") {
		return Discovery{}, ErrDiscoveryEndpoint
	}
	// Policy: PKCE is mandatory for this public client, so S256 must be advertised.
	// An absent member means "not advertised", which is equally unusable here.
	pkce, err := stringsMember(members, memberPKCE)
	if err != nil {
		return Discovery{}, err
	}
	if !contains(pkce, "S256") {
		return Discovery{}, ErrDiscoveryPKCE
	}
	// Policy: only a public client is modelled. When the server lists methods, "none"
	// must be among them; an omitted member is tolerated.
	authMethods, err := stringsMember(members, memberTokenAuthMethods)
	if err != nil {
		return Discovery{}, err
	}
	if len(authMethods) > 0 && !containsFold(authMethods, "none") {
		return Discovery{}, ErrDiscoveryClientAuth
	}

	discovery := Discovery{
		Issuer:                issuer,
		AuthorizationEndpoint: authorization,
		TokenEndpoint:         token,
	}
	if rawRevocation, present := members[memberRevocation]; present {
		revocation, err := decodeString(rawRevocation)
		if err != nil {
			return Discovery{}, err
		}
		if revocation != "" {
			if !acceptableEndpoint(revocation) {
				return Discovery{}, ErrDiscoveryEndpoint
			}
			discovery.RevocationEndpoint = revocation
		}
	}
	scopes, err := stringsMember(members, memberScopes)
	if err != nil {
		return Discovery{}, err
	}
	for _, scope := range scopes {
		if !validScopeToken(scope) {
			return Discovery{}, ErrDiscoveryDocument
		}
	}
	discovery.Scopes = scopes
	return discovery, nil
}

func acceptableEndpoint(value string) bool {
	return value != "" && len(value) <= maxEndpointLength && secureEndpoint(value)
}

// documentMembers decodes the top level as raw members so that only exactly-named
// members are ever consulted.
func documentMembers(raw []byte) (map[string]json.RawMessage, error) {
	var members map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&members); err != nil || members == nil {
		return nil, ErrDiscoveryDocument
	}
	// Decode stops at the end of the first value; anything after it means these bytes are
	// not a single metadata document, even if the first value looked valid.
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrDiscoveryDocument
	}
	return members, nil
}

func decodeString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", ErrDiscoveryDocument
	}
	return value, nil
}

func stringMember(members map[string]json.RawMessage, name string) (string, error) {
	raw, present := members[name]
	if !present {
		return "", ErrDiscoveryDocument
	}
	return decodeString(raw)
}

// stringsMember reads an optional string array; an absent member is simply empty, which
// the individual callers then judge (no code flow, no S256, no advertised methods).
func stringsMember(members map[string]json.RawMessage, name string) ([]string, error) {
	raw, present := members[name]
	if !present {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, ErrDiscoveryDocument
	}
	return values, nil
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
			if _, err := decoder.Token(); err != nil { // closing brace
				return ErrDiscoveryDocument
			}
			return nil
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			if _, err := decoder.Token(); err != nil { // closing bracket
				return ErrDiscoveryDocument
			}
			return nil
		}
		return ErrDiscoveryDocument
	}
	if err := walk(); err != nil {
		return err
	}
	// walk consumed exactly one value. A second Token() must report EOF; any value means
	// trailing content, and a syntax error means the bytes were already broken.
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrDiscoveryDocument
	}
	return nil
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
