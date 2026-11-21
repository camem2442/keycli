package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
)

var cf = Provider{Issuer: "https://owner.cloudflareaccess.com", AuthorizationEndpoint: "https://auth.example.test/authorize", TokenEndpoint: "https://auth.example.test/token"}
var native = Client{ID: "mobile-public", RedirectURI: "com.example.usage:/oauth/callback", Resource: "https://ai-usage.example.test", Scopes: []string{"openid", "profile"}}

func begin(t *testing.T) (string, Pending) {
	t.Helper()
	auth, pending, err := Begin(cf, native, strings.NewReader(strings.Repeat("Z", 32)+strings.Repeat("Y", 32)))
	if err != nil {
		t.Fatal(err)
	}
	return auth, pending
}

func TestPKCEAndPinnedResource(t *testing.T) {
	auth, p := begin(t)
	u, _ := url.Parse(auth)
	q := u.Query()
	sum := sha256.Sum256([]byte(p.CodeVerifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) || q.Get("code_challenge_method") != "S256" || q.Get("response_type") != "code" || q.Get("state") != p.State || q.Get("resource") != native.Resource || q.Get("redirect_uri") != native.RedirectURI {
		t.Fatal("bad PKCE request")
	}
	if len(p.State) != 43 || len(p.CodeVerifier) != 43 || p.State == p.CodeVerifier {
		t.Fatal("bad entropy")
	}
}

func TestCallbackValidAndRejectsSmuggling(t *testing.T) {
	_, p := begin(t)
	good := native.RedirectURI + "?code=one&state=" + url.QueryEscape(p.State)
	if code, err := ParseCallback(p, good); err != nil || code != "one" {
		t.Fatal(code, err)
	}
	if code, err := ParseCallback(p, good); err != nil || code != "one" {
		t.Fatal("second read", code, err)
	}
	for _, candidate := range []string{
		native.RedirectURI + "?code=one&state=wrong",
		native.RedirectURI + "?code=one&code=two&state=" + p.State,
		native.RedirectURI + "?code=one&state=" + p.State + "&state=" + p.State,
		native.RedirectURI + "?code=one&state=" + p.State + "&authorization=secret",
		native.RedirectURI + "?state=" + p.State,
		native.RedirectURI + "?code=one&state=" + p.State + "#fragment",
		"com.example.other:/oauth/callback?code=one&state=" + p.State,
		native.RedirectURI + "/extra?code=one&state=" + p.State,
		native.RedirectURI + "?code=one&state=" + p.State + "&iss=https%3A%2F%2Fevil.example",
		native.RedirectURI + "?code=one&state=" + p.State + "&error=access_denied",
	} {
		if _, err := ParseCallback(p, candidate); !errors.Is(err, ErrCallback) {
			t.Fatalf("admitted %q: %v", candidate, err)
		}
	}
	if _, err := ParseCallback(p, good+"&iss="+url.QueryEscape(cf.Issuer)); err != nil {
		t.Fatal(err)
	}
}

func TestRefuseUnsafeConfiguration(t *testing.T) {
	cases := []struct {
		p Provider
		c Client
	}{
		{Provider{Issuer: "http://owner.cloudflareaccess.com", AuthorizationEndpoint: cf.AuthorizationEndpoint, TokenEndpoint: cf.TokenEndpoint}, native},
		{Provider{Issuer: cf.Issuer, AuthorizationEndpoint: "http://auth.example.test/a", TokenEndpoint: cf.TokenEndpoint}, native},
		{cf, Client{ID: native.ID, RedirectURI: "https://api.example.test/callback", Resource: "http://ai-usage.example.test"}},
		{cf, Client{ID: native.ID, RedirectURI: "http://192.0.2.7/callback"}},
		{cf, Client{ID: native.ID, RedirectURI: "evil:/callback"}},
		{cf, Client{ID: native.ID, RedirectURI: native.RedirectURI + "?code=x"}},
		{cf, Client{ID: native.ID, RedirectURI: native.RedirectURI, Scopes: []string{"scope injection"}}},
	}
	for i, x := range cases {
		if _, _, err := Begin(x.p, x.c, nil); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("case %d accepted", i)
		}
	}
}

func TestNoEntropyFailsClosed(t *testing.T) {
	if _, _, err := Begin(cf, native, strings.NewReader("")); !errors.Is(err, ErrConfiguration) {
		t.Fatal(err)
	}
}

func TestMalformedPendingRefused(t *testing.T) {
	_, p := begin(t)
	good := native.RedirectURI + "?code=one&state=" + url.QueryEscape(p.State)
	p.CodeVerifier = "short"
	if _, err := ParseCallback(p, good); !errors.Is(err, ErrCallback) {
		t.Fatal("accepted malformed pending verifier")
	}
	p.CodeVerifier = strings.Repeat("Z", 43)
	p.State = "short"
	if _, err := ParseCallback(p, good); !errors.Is(err, ErrCallback) {
		t.Fatal("accepted malformed pending state")
	}
}
