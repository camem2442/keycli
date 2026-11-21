// Package oauth prepares native public-client authorization-code requests.
// It performs no network, browser, filesystem, or secret-store operations.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"regexp"
	"strings"
)

var (
	ErrConfiguration = errors.New("oauth: invalid configuration")
	ErrCallback      = errors.New("oauth: callback rejected")
)

var privateScheme = regexp.MustCompile("^[a-z][a-z0-9+.-]*\\.[a-z0-9+.-]+$")

// Provider contains trusted, independently verified authorization server metadata.
// This package does not perform discovery or accept arbitrary metadata URLs.
type Provider struct {
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
}

type Client struct {
	ID          string
	RedirectURI string
	Resource    string // RFC 8707; required for Cloudflare Managed OAuth adapter
	Scopes      []string
}

// Pending is secret-bearing ephemeral state. It must never be logged or committed.
type Pending struct {
	State        string
	CodeVerifier string
	RedirectURI  string
	Issuer       string
	Resource     string
}

func secureEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" &&
		u.User == nil && u.Fragment == "" && u.RawQuery == "" && u.Opaque == "" &&
		u.Port() != "0" && !strings.HasSuffix(u.Host, ":")
}

func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" || u.Path == "" {
		return false
	}
	if u.Scheme == "https" {
		return u.Hostname() != ""
	}
	if u.Scheme == "http" {
		return u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" || u.Hostname() == "localhost"
	}
	return privateScheme.MatchString(u.Scheme) && u.Host == "" && strings.HasPrefix(u.Path, "/")
}

// Begin creates RFC 7636 S256 PKCE and CSRF state for an RFC 8252 native app.
// The caller owns external-browser opening, callback transport, and token exchange.
func Begin(provider Provider, client Client, entropy io.Reader) (string, Pending, error) {
	if !secureEndpoint(provider.Issuer) || !secureEndpoint(provider.AuthorizationEndpoint) ||
		!secureEndpoint(provider.TokenEndpoint) || !validRedirect(client.RedirectURI) ||
		strings.TrimSpace(client.ID) != client.ID || client.ID == "" ||
		(client.Resource != "" && !secureEndpoint(client.Resource)) {
		return "", Pending{}, ErrConfiguration
	}
	for _, scope := range client.Scopes {
		if scope == "" || strings.ContainsAny(scope, " \r\n\t") {
			return "", Pending{}, ErrConfiguration
		}
	}
	if entropy == nil {
		entropy = rand.Reader
	}
	verifierBytes, stateBytes := make([]byte, 32), make([]byte, 32)
	if _, err := io.ReadFull(entropy, verifierBytes); err != nil {
		return "", Pending{}, ErrConfiguration
	}
	if _, err := io.ReadFull(entropy, stateBytes); err != nil {
		return "", Pending{}, ErrConfiguration
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	digest := sha256.Sum256([]byte(verifier))
	target, _ := url.Parse(provider.AuthorizationEndpoint)
	query := target.Query()
	query.Set("response_type", "code")
	query.Set("client_id", client.ID)
	query.Set("redirect_uri", client.RedirectURI)
	query.Set("code_challenge_method", "S256")
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
	query.Set("state", state)
	if client.Resource != "" {
		query.Set("resource", client.Resource)
	}
	if len(client.Scopes) != 0 {
		query.Set("scope", strings.Join(client.Scopes, " "))
	}
	target.RawQuery = query.Encode()
	return target.String(), Pending{State: state, CodeVerifier: verifier, RedirectURI: client.RedirectURI, Issuer: provider.Issuer, Resource: client.Resource}, nil
}

// ParseCallback rejects mismatched destinations, duplicate parameters and state replay.
// Callers must consume/delete Pending once, even on failure.
func ParseCallback(p Pending, callback string) (string, error) {
	want, err := url.Parse(p.RedirectURI)
	if err != nil || !validRedirect(p.RedirectURI) || p.State == "" || p.Issuer == "" {
		return "", ErrCallback
	}
	got, err := url.Parse(callback)
	if err != nil || got.Scheme != want.Scheme || got.Host != want.Host ||
		got.EscapedPath() != want.EscapedPath() || got.Opaque != want.Opaque ||
		got.User != nil || got.Fragment != "" || got.RawFragment != "" || got.ForceQuery {
		return "", ErrCallback
	}
	values, err := url.ParseQuery(got.RawQuery)
	if err != nil {
		return "", ErrCallback
	}
	for key, fields := range values {
		if len(fields) != 1 || (key != "code" && key != "state" && key != "iss") {
			return "", ErrCallback
		}
	}
	if len(values["state"]) != 1 || len(values["code"]) != 1 || values.Get("code") == "" ||
		subtle.ConstantTimeCompare([]byte(values.Get("state")), []byte(p.State)) != 1 ||
		(values.Has("iss") && values.Get("iss") != p.Issuer) {
		return "", ErrCallback
	}
	return values.Get("code"), nil
}
