package oauth_test

import (
    "strings"
    "testing"

    oauth "github.com/camem2442/keycli/sdk/portableoauth"
)

// Import through the *public module path*, not the owner-only internal path.
func TestCandidateExternalImportAndPKCECallback(t *testing.T) {
    provider := oauth.Provider{
        Issuer: "https://fixture.cloudflareaccess.com",
        AuthorizationEndpoint: "https://fixture.cloudflareaccess.com/oauth/authorize",
        TokenEndpoint: "https://fixture.cloudflareaccess.com/oauth/token",
    }
    client := oauth.Client{
        ID: "fixture-public-client",
        RedirectURI: "local.k.ios.aiusage:/oauth/callback",
        Resource: "https://usage.example.invalid",
    }
    uri, pending, err := oauth.Begin(provider, client, strings.NewReader(strings.Repeat("Z", 32)+strings.Repeat("Y", 32)))
    if err != nil || !strings.Contains(uri, "code_challenge_method=S256") {
        t.Fatalf("public OAuth Begin failed: %v", err)
    }
    callback := client.RedirectURI + "?code=fixture&state=" + pending.State
    code, err := oauth.ParseCallback(pending, callback)
    if err != nil || code != "fixture" {
        t.Fatal("public callback parser rejected exact registered callback")
    }
    // This remains a pure parser; reuse is NOT a grant or a complete login.
    if again, err := oauth.ParseCallback(pending, callback); err != nil || again != code {
        t.Fatal("parser contract must not masquerade as single-use state machine")
    }
}
