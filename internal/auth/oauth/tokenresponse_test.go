package oauth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

var fixedNow = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func tokenDocument(fields map[string]string) []byte {
	parts := make([]string, 0, len(fields))
	for key, value := range fields {
		parts = append(parts, `"`+key+`":`+value)
	}
	return []byte("{" + strings.Join(parts, ",") + "}")
}

func TestParseTokenResponseAcceptsASuccessDocument(t *testing.T) {
	raw := tokenDocument(map[string]string{
		"access_token":  `"at-1"`,
		"token_type":    `"Bearer"`,
		"expires_in":    `3600`,
		"refresh_token": `"rt-1"`,
		"scope":         `"usage:read files:read"`,
	})
	tokens, err := ParseTokenResponse(raw, fixedNow)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if tokens.AccessToken != "at-1" || tokens.RefreshToken != "rt-1" {
		t.Fatalf("credentials lost: %+v", tokens)
	}
	if tokens.TokenType != "Bearer" {
		t.Fatalf("token type lost: %q", tokens.TokenType)
	}
	if want := fixedNow.Add(time.Hour); !tokens.ExpiresAt.Equal(want) {
		t.Fatalf("expiry: want %v, got %v", want, tokens.ExpiresAt)
	}
	if tokens.Scope != "usage:read files:read" {
		t.Fatalf("scope lost: %q", tokens.Scope)
	}
}

func TestParseTokenResponseToleratesOptionalMembers(t *testing.T) {
	// No refresh token and no expiry is legal on the wire. The caller learns from
	// Tokens.ExpiresAt that the access token's lifetime is unknown.
	tokens, err := ParseTokenResponse(tokenDocument(map[string]string{"access_token": `"at"`}), fixedNow)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !tokens.ExpiresAt.IsZero() || tokens.RefreshToken != "" {
		t.Fatalf("absent members must stay absent: %+v", tokens)
	}
	// An omitted token_type is tolerated; a different one is not.
	if _, err := ParseTokenResponse(tokenDocument(map[string]string{"access_token": `"at"`, "token_type": `"bearer"`}), fixedNow); err != nil {
		t.Fatalf("case-insensitive bearer must be accepted: %v", err)
	}
	if _, err := ParseTokenResponse(tokenDocument(map[string]string{"access_token": `"at"`, "token_type": `"mac"`}), fixedNow); !errors.Is(err, ErrTokenResponse) {
		t.Fatalf("want ErrTokenResponse, got %v", err)
	}
}

func TestParseTokenResponseRejectsUnusableCredentials(t *testing.T) {
	long := strings.Repeat("a", MaxTokenBytes+1)
	cases := map[string][]byte{
		"missing access token":      tokenDocument(map[string]string{"token_type": `"Bearer"`}),
		"empty access token":        tokenDocument(map[string]string{"access_token": `""`}),
		"access token with space":   tokenDocument(map[string]string{"access_token": `"a b"`}),
		"access token with newline": tokenDocument(map[string]string{"access_token": `"a\nb"`}),
		"oversized access token":    tokenDocument(map[string]string{"access_token": `"` + long + `"`}),
		"refresh token with space":  tokenDocument(map[string]string{"access_token": `"at"`, "refresh_token": `"r t"`}),
		"access token not a string": tokenDocument(map[string]string{"access_token": `1`}),
		"duplicate access token":    []byte(`{"access_token":"a","access_token":"b"}`),
		"not an object":             []byte(`["at"]`),
		"empty":                     nil,
		"truncated":                 []byte(`{"access_token":"a"`),
		"trailing content":          append(tokenDocument(map[string]string{"access_token": `"at"`}), []byte(` {}`)...),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseTokenResponse(raw, fixedNow); !errors.Is(err, ErrTokenResponse) {
				t.Fatalf("want ErrTokenResponse, got %v", err)
			}
		})
	}
}

func TestParseTokenResponseBoundsExpiresIn(t *testing.T) {
	base := map[string]string{"access_token": `"at"`}
	for name, value := range map[string]string{
		"string":   `"3600"`,
		"float":    `3600.5`,
		"exponent": `3.6e3`,
		"zero":     `0`,
		"negative": `-1`,
		"too far":  `31622401`,
	} {
		t.Run(name, func(t *testing.T) {
			fields := map[string]string{"access_token": base["access_token"], "expires_in": value}
			if _, err := ParseTokenResponse(tokenDocument(fields), fixedNow); !errors.Is(err, ErrTokenResponse) {
				t.Fatalf("want ErrTokenResponse, got %v", err)
			}
		})
	}
	fields := map[string]string{"access_token": `"at"`, "expires_in": `31622400`}
	if _, err := ParseTokenResponse(tokenDocument(fields), fixedNow); err != nil {
		t.Fatalf("the documented maximum must be accepted: %v", err)
	}
}

// The error member ends up inside an error string. A hostile body must not be able to
// put control bytes or a long arbitrary string into a log line.
func TestParseTokenErrorSanitisesHostileCodes(t *testing.T) {
	for name, raw := range map[string][]byte{
		"newline":   tokenDocument(map[string]string{"error": `"bad\ncode"`}),
		"carriage":  tokenDocument(map[string]string{"error": `"bad\rcode"`}),
		"quote":     tokenDocument(map[string]string{"error": `"bad\"code"`}),
		"backslash": tokenDocument(map[string]string{"error": `"bad\\code"`}),
		"very long": tokenDocument(map[string]string{"error": `"` + strings.Repeat("a", 65) + `"`}),
		"empty":     tokenDocument(map[string]string{"error": `""`}),
	} {
		t.Run(name, func(t *testing.T) {
			err := ParseTokenError(raw)
			if !errors.Is(err, ErrTokenResponse) {
				t.Fatalf("want ErrTokenResponse, got %v", err)
			}
			if strings.ContainsAny(err.Error(), "\r\n") {
				t.Fatalf("error contains a control character: %q", err.Error())
			}
			if strings.Contains(err.Error(), strings.Repeat("a", 65)) {
				t.Fatalf("error echoed an over-long code: %v", err)
			}
		})
	}
}

func TestParseTokenResponseRejectsMalformedScope(t *testing.T) {
	for name, scope := range map[string]string{
		"quote":     `"bad\"scope"`,
		"tab":       `"a\tb"`,
		"newline":   `"a\nb"`,
		"carriage":  `"a\rb"`,
		"backslash": `"a\\b"`,
	} {
		t.Run(name, func(t *testing.T) {
			fields := map[string]string{"access_token": `"at"`, "scope": scope}
			if _, err := ParseTokenResponse(tokenDocument(fields), fixedNow); !errors.Is(err, ErrTokenResponse) {
				t.Fatalf("want ErrTokenResponse, got %v", err)
			}
		})
	}
}

func TestParseTokenResponseClassifiesErrorDocuments(t *testing.T) {
	cases := map[string]struct {
		code   string
		target error
	}{
		"invalid_grant":           {"invalid_grant", ErrTokenRejected},
		"invalid_client":          {"invalid_client", ErrTokenRejected},
		"unauthorized_client":     {"unauthorized_client", ErrTokenRejected},
		"invalid_scope":           {"invalid_scope", ErrTokenRejected},
		"unsupported_grant_type":  {"unsupported_grant_type", ErrTokenRejected},
		"server_error":            {"server_error", ErrTokenTransient},
		"temporarily_unavailable": {"temporarily_unavailable", ErrTokenTransient},
		"unknown code":            {"something_new", ErrTokenResponse},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			raw := tokenDocument(map[string]string{"error": `"` + test.code + `"`})
			if _, err := ParseTokenResponse(raw, fixedNow); !errors.Is(err, test.target) {
				t.Fatalf("want %v, got %v", test.target, err)
			}
		})
	}
}

func TestParseTokenResponseRefusesAmbiguousErrorDocument(t *testing.T) {
	// A document that fails and also carries a credential cannot be trusted either way.
	raw := tokenDocument(map[string]string{"error": `"invalid_grant"`, "access_token": `"at"`})
	if _, err := ParseTokenResponse(raw, fixedNow); !errors.Is(err, ErrTokenResponse) {
		t.Fatalf("want ErrTokenResponse, got %v", err)
	}
	if err := ParseTokenError(raw); !errors.Is(err, ErrTokenResponse) {
		t.Fatalf("want ErrTokenResponse from ParseTokenError, got %v", err)
	}
}

func TestParseTokenErrorDoesNotEchoServerText(t *testing.T) {
	raw := tokenDocument(map[string]string{
		"error":             `"invalid_grant"`,
		"error_description": `"secret-looking detail"`,
	})
	err := ParseTokenError(raw)
	if !errors.Is(err, ErrTokenRejected) {
		t.Fatalf("want ErrTokenRejected, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-looking") {
		t.Fatalf("description must not be echoed: %v", err)
	}
}

func TestParseTokenErrorRefusesSuccessDocuments(t *testing.T) {
	raw := tokenDocument(map[string]string{"access_token": `"at"`})
	if err := ParseTokenError(raw); !errors.Is(err, ErrTokenResponse) {
		t.Fatalf("want ErrTokenResponse, got %v", err)
	}
}

func TestParseTokenResponseRejectsOversizedDocument(t *testing.T) {
	raw := []byte(`{"access_token":"` + strings.Repeat("a", MaxTokenResponseBytes) + `"}`)
	if _, err := ParseTokenResponse(raw, fixedNow); !errors.Is(err, ErrTokenResponse) {
		t.Fatalf("want ErrTokenResponse, got %v", err)
	}
}

func TestTokensFromResponseAreNotSerializable(t *testing.T) {
	tokens, err := ParseTokenResponse(tokenDocument(map[string]string{"access_token": `"at"`}), fixedNow)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, err := json.Marshal(tokens); !errors.Is(err, ErrTokenRedacted) {
		t.Fatalf("want ErrTokenRedacted, got %v", err)
	}
	if strings.Contains(tokens.String(), "at") {
		t.Fatalf("String leaked the token: %s", tokens.String())
	}
}
