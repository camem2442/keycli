package oauth

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func exchangeFixture() (Provider, Client, Pending) {
	provider := Provider{
		Issuer:        "https://access.example.com",
		TokenEndpoint: "https://access.example.com/token",
	}
	client := Client{
		ID:          "client-id",
		RedirectURI: "com.example.app:/callback",
	}
	pending := Pending{
		State:        "state",
		CodeVerifier: strings.Repeat("v", 43),
		RedirectURI:  client.RedirectURI,
		Issuer:       provider.Issuer,
	}
	return provider, client, pending
}

func TestBuildExchangeProducesTheRFC6749Form(t *testing.T) {
	provider, client, pending := exchangeFixture()
	form, err := BuildExchange(provider, client, pending, "the-code")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	want := map[string]string{
		"grant_type":    "authorization_code",
		"code":          "the-code",
		"redirect_uri":  "com.example.app:/callback",
		"client_id":     "client-id",
		"code_verifier": strings.Repeat("v", 43),
	}
	if len(form) != len(want) {
		t.Fatalf("unexpected members: %v", form)
	}
	for key, value := range want {
		if got := form.Get(key); got != value {
			t.Fatalf("%s: want %q, got %q", key, value, got)
		}
	}
	// scope belongs to the authorization request, not the code exchange.
	if form.Has("scope") {
		t.Fatalf("code exchange must not resend scope: %v", form)
	}
}

func TestBuildExchangeRepeatsTheResourceIndicator(t *testing.T) {
	provider, client, pending := exchangeFixture()
	client.Resource = "https://api.example.com"
	pending.Resource = client.Resource
	form, err := BuildExchange(provider, client, pending, "code")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := form.Get("resource"); got != client.Resource {
		t.Fatalf("want the same resource as the authorization request, got %q", got)
	}
}

func TestBuildExchangeRefusesMismatchedState(t *testing.T) {
	provider, client, pending := exchangeFixture()
	cases := map[string]func() (Provider, Client, Pending, string){
		"issuer mismatch": func() (Provider, Client, Pending, string) {
			p := pending
			p.Issuer = "https://other.example"
			return provider, client, p, "code"
		},
		"empty pending issuer": func() (Provider, Client, Pending, string) {
			p := pending
			p.Issuer = ""
			return provider, client, p, "code"
		},
		"redirect mismatch": func() (Provider, Client, Pending, string) {
			return provider, client, Pending{State: "s", CodeVerifier: pending.CodeVerifier, RedirectURI: "com.example.app:/other", Issuer: provider.Issuer}, "code"
		},
		"resource mismatch": func() (Provider, Client, Pending, string) {
			p := pending
			p.Resource = "https://api.example.com"
			return provider, client, p, "code"
		},
		"insecure resource": func() (Provider, Client, Pending, string) {
			client.Resource = "http://api.example.com"
			p := pending
			p.Resource = client.Resource
			return provider, client, p, "code"
		},
		"insecure token endpoint": func() (Provider, Client, Pending, string) {
			p := provider
			p.TokenEndpoint = "http://access.example.com/token"
			return p, client, pending, "code"
		},
		"insecure issuer": func() (Provider, Client, Pending, string) {
			p := provider
			p.Issuer = "http://access.example.com"
			// Keep Pending in step, otherwise the equality check fires first and the
			// issuer's own scheme is never examined.
			pending.Issuer = p.Issuer
			return p, client, pending, "code"
		},
		"client id with space": func() (Provider, Client, Pending, string) {
			c := client
			c.ID = " client-id"
			return provider, c, pending, "code"
		},
		"empty code": func() (Provider, Client, Pending, string) {
			return provider, client, pending, ""
		},
		"code with space": func() (Provider, Client, Pending, string) {
			return provider, client, pending, "a b"
		},
		"oversized code": func() (Provider, Client, Pending, string) {
			return provider, client, pending, strings.Repeat("c", MaxAuthorizationCodeBytes+1)
		},
		"short verifier": func() (Provider, Client, Pending, string) {
			p := pending
			p.CodeVerifier = strings.Repeat("v", 42)
			return provider, client, p, "code"
		},
		"long verifier": func() (Provider, Client, Pending, string) {
			p := pending
			p.CodeVerifier = strings.Repeat("v", 129)
			return provider, client, p, "code"
		},
		"verifier with illegal character": func() (Provider, Client, Pending, string) {
			p := pending
			p.CodeVerifier = strings.Repeat("v", 42) + "!"
			return provider, client, p, "code"
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			provider, client, pending, code := build()
			if form, err := BuildExchange(provider, client, pending, code); !errors.Is(err, ErrExchange) {
				t.Fatalf("want ErrExchange, got form=%v err=%v", form, err)
			}
		})
	}
}

func TestBuildExchangeDoesNotEscapeTheVerifier(t *testing.T) {
	// RFC 7636 verifiers are unreserved characters, so encoding must leave them intact.
	provider, client, pending := exchangeFixture()
	pending.CodeVerifier = strings.Repeat("a", 40) + "-._~"
	form, err := BuildExchange(provider, client, pending, "code")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	encoded := form.Encode()
	if !strings.Contains(encoded, "code_verifier="+pending.CodeVerifier) {
		t.Fatalf("verifier was altered in the form: %s", encoded)
	}
	parsed, err := url.ParseQuery(encoded)
	if err != nil {
		t.Fatalf("form does not parse: %v", err)
	}
	if got := parsed.Get("code_verifier"); got != pending.CodeVerifier {
		t.Fatalf("round trip changed the verifier: %q", got)
	}
}
