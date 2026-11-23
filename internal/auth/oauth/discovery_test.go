package oauth

import (
	"errors"
	"strings"
	"testing"
)

const pinned = "https://access.example.com"

func metadata(overrides map[string]string) []byte {
	fields := map[string]string{
		"issuer":                                `"` + pinned + `"`,
		"authorization_endpoint":                `"` + pinned + `/cdn-cgi/access/oauth/authorization"`,
		"token_endpoint":                        `"` + pinned + `/cdn-cgi/access/oauth/token"`,
		"response_types_supported":              `["code"]`,
		"code_challenge_methods_supported":      `["S256"]`,
		"token_endpoint_auth_methods_supported": `["none"]`,
	}
	for key, value := range overrides {
		if value == "" {
			delete(fields, key)
			continue
		}
		fields[key] = value
	}
	parts := make([]string, 0, len(fields))
	for key, value := range fields {
		parts = append(parts, `"`+key+`":`+value)
	}
	return []byte("{" + strings.Join(parts, ",") + "}")
}

func TestParseDiscoveryAcceptsAWellFormedDocument(t *testing.T) {
	discovery, err := ParseDiscovery(metadata(map[string]string{
		"revocation_endpoint": `"` + pinned + `/cdn-cgi/access/oauth/revoke"`,
		"scopes_supported":    `["usage:read"]`,
	}), pinned)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if discovery.RevocationEndpoint == "" || len(discovery.Scopes) != 1 {
		t.Fatalf("optional members dropped: %+v", discovery)
	}
	provider := discovery.Provider()
	if provider.Issuer != pinned || provider.AuthorizationEndpoint == "" || provider.TokenEndpoint == "" {
		t.Fatalf("provider mapping wrong: %+v", provider)
	}
}

func TestParseDiscoveryRequiresTheExactPinnedIssuer(t *testing.T) {
	for name, issuer := range map[string]string{
		"trailing slash": pinned + "/",
		"other host":     "https://access.evil.example",
		"http scheme":    "http://access.example.com",
		"with fragment":  pinned + "#x",
		"with query":     pinned + "?x=1",
		"missing":        "",
	} {
		t.Run(name, func(t *testing.T) {
			raw := metadata(map[string]string{"issuer": `"` + issuer + `"`})
			if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryIssuer) {
				t.Fatalf("want ErrDiscoveryIssuer, got %v", err)
			}
		})
	}
}

func TestParseDiscoveryRejectsUnacceptableEndpoints(t *testing.T) {
	cases := map[string][]byte{
		"http authorization":  metadata(map[string]string{"authorization_endpoint": `"http://access.example.com/a"`}),
		"userinfo token":      metadata(map[string]string{"token_endpoint": `"https://user:pw@access.example.com/t"`}),
		"token with query":    metadata(map[string]string{"token_endpoint": `"https://access.example.com/t?x=1"`}),
		"token with fragment": metadata(map[string]string{"token_endpoint": `"https://access.example.com/t#f"`}),
		"same endpoint":       metadata(map[string]string{"token_endpoint": `"` + pinned + `/cdn-cgi/access/oauth/authorization"`}),
		"bad revocation":      metadata(map[string]string{"revocation_endpoint": `"http://access.example.com/r"`}),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryEndpoint) {
				t.Fatalf("want ErrDiscoveryEndpoint, got %v", err)
			}
		})
	}
}

func TestParseDiscoveryRequiresCodeFlow(t *testing.T) {
	for name, value := range map[string]string{
		"missing":  "",
		"implicit": `["token"]`,
	} {
		t.Run(name, func(t *testing.T) {
			raw := metadata(map[string]string{"response_types_supported": value})
			if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryEndpoint) {
				t.Fatalf("want ErrDiscoveryEndpoint, got %v", err)
			}
		})
	}
}

func TestParseDiscoveryRequiresS256(t *testing.T) {
	for name, value := range map[string]string{
		"missing": `["plain"]`,
		"empty":   `[]`,
		"absent":  "",
	} {
		t.Run(name, func(t *testing.T) {
			raw := metadata(map[string]string{"code_challenge_methods_supported": value})
			if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryPKCE) {
				t.Fatalf("want ErrDiscoveryPKCE, got %v", err)
			}
		})
	}
}

func TestParseDiscoveryRequiresPublicClientSupport(t *testing.T) {
	raw := metadata(map[string]string{"token_endpoint_auth_methods_supported": `["client_secret_basic"]`})
	if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryClientAuth) {
		t.Fatalf("want ErrDiscoveryClientAuth, got %v", err)
	}
	// Omitted means "not advertised", which RFC 8414 permits; the client may still try.
	if _, err := ParseDiscovery(metadata(map[string]string{"token_endpoint_auth_methods_supported": ""}), pinned); err != nil {
		t.Fatalf("omitted auth methods must be tolerated: %v", err)
	}
	// Case-insensitive match.
	if _, err := ParseDiscovery(metadata(map[string]string{"token_endpoint_auth_methods_supported": `["NONE"]`}), pinned); err != nil {
		t.Fatalf("NONE must be accepted: %v", err)
	}
}

func TestParseDiscoveryRejectsAmbiguousDocuments(t *testing.T) {
	cases := map[string][]byte{
		"duplicate issuer": []byte(`{"issuer":"` + pinned + `","issuer":"https://evil.example",` +
			`"authorization_endpoint":"` + pinned + `/a","token_endpoint":"` + pinned + `/t",` +
			`"response_types_supported":["code"],"code_challenge_methods_supported":["S256"]}`),
		"not an object":    []byte(`["issuer"]`),
		"trailing content": append(metadata(nil), []byte(` {}`)...),
		"invalid json":     []byte(`{"issuer":`),
		"empty":            nil,
		"invalid scope":    metadata(map[string]string{"scopes_supported": `["bad\"scope"]`}),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryDocument) {
				t.Fatalf("want ErrDiscoveryDocument, got %v", err)
			}
		})
	}
}

func TestParseDiscoveryRejectsOversizedDocument(t *testing.T) {
	raw := make([]byte, MaxDiscoveryBytes+1)
	for i := range raw {
		raw[i] = 'x'
	}
	if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryDocument) {
		t.Fatalf("want ErrDiscoveryDocument, got %v", err)
	}
}

func TestParseDiscoveryToleratesUnknownMembers(t *testing.T) {
	raw := append(metadata(map[string]string{"future_member": `["x"]`}), []byte{}...)
	if _, err := ParseDiscovery(raw, pinned); err != nil {
		t.Fatalf("extensions must be ignored: %v", err)
	}
}

// encoding/json matches struct fields case-insensitively, so a document could carry a
// benign member and a differently-cased hostile twin. Only exactly-named members are read.
func TestParseDiscoveryIgnoresDifferentlyCasedMembers(t *testing.T) {
	raw := []byte(`{"issuer":"` + pinned + `",` +
		`"authorization_endpoint":"` + pinned + `/a",` +
		`"token_endpoint":"` + pinned + `/t",` +
		`"Token_Endpoint":"https://evil.example/t",` +
		`"response_types_supported":["code"],` +
		`"code_challenge_methods_supported":["S256"]}`)
	discovery, err := ParseDiscovery(raw, pinned)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if discovery.TokenEndpoint != pinned+"/t" {
		t.Fatalf("a differently-cased member must not win, got %s", discovery.TokenEndpoint)
	}
}

// A document that only carries the wrong case is simply missing the required member.
func TestParseDiscoveryRequiresExactMemberNames(t *testing.T) {
	raw := []byte(`{"issuer":"` + pinned + `",` +
		`"Authorization_Endpoint":"` + pinned + `/a",` +
		`"token_endpoint":"` + pinned + `/t",` +
		`"response_types_supported":["code"],` +
		`"code_challenge_methods_supported":["S256"]}`)
	if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryDocument) {
		t.Fatalf("want ErrDiscoveryDocument, got %v", err)
	}
}

func TestParseDiscoveryIssuerIsCaseSensitive(t *testing.T) {
	raw := metadata(map[string]string{"issuer": `"https://ACCESS.example.com"`})
	if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryIssuer) {
		t.Fatalf("want ErrDiscoveryIssuer, got %v", err)
	}
}

func TestParseDiscoveryRejectsZeroPortEndpoint(t *testing.T) {
	raw := metadata(map[string]string{"token_endpoint": `"https://access.example.com:0/t"`})
	if _, err := ParseDiscovery(raw, pinned); !errors.Is(err, ErrDiscoveryEndpoint) {
		t.Fatalf("want ErrDiscoveryEndpoint, got %v", err)
	}
}

func TestParseDiscoveryRefusesUntrustworthyPinnedIssuer(t *testing.T) {
	for name, issuer := range map[string]string{
		"http":     "http://access.example.com",
		"userinfo": "https://user:pw@access.example.com",
		"empty":    "",
		"query":    pinned + "?a=1",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseDiscovery(metadata(nil), issuer); !errors.Is(err, ErrDiscoveryIssuer) {
				t.Fatalf("want ErrDiscoveryIssuer, got %v", err)
			}
		})
	}
}

func TestDiscoveryURLFollowsRFC8414PathRules(t *testing.T) {
	cases := map[string]string{
		pinned:              pinned + "/.well-known/oauth-authorization-server",
		pinned + "/":        pinned + "/.well-known/oauth-authorization-server",
		pinned + "/tenant":  pinned + "/.well-known/oauth-authorization-server/tenant",
		pinned + "/tenant/": pinned + "/.well-known/oauth-authorization-server/tenant",
	}
	for issuer, want := range cases {
		got, err := DiscoveryURL(issuer)
		if err != nil {
			t.Fatalf("%s: unexpected %v", issuer, err)
		}
		if got != want {
			t.Fatalf("%s: want %s, got %s", issuer, want, got)
		}
	}
	for _, issuer := range []string{"http://access.example.com", "https://user:pw@access.example.com", ""} {
		if _, err := DiscoveryURL(issuer); !errors.Is(err, ErrDiscoveryIssuer) {
			t.Fatalf("%q must be refused, got %v", issuer, err)
		}
	}
}
