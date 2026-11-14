// Package ociwif verifies an OCI instance-principal identity token and exchanges
// it for a short-lived OpenAI access token.
//
// The flow is the one documented by OpenAI for Oracle Cloud Infrastructure:
//
//	OCI instance principal
//	  → identity-domain access token (subject token)
//	  → OpenAI workload identity federation
//	  → short-lived OpenAI service-account token
//
// This package never calls a provider in tests and
// never replaces the token audience with an OpenAI URL. The Oracle audience that
// is actually issued is the only acceptable one, because an audience rewrite
// would let an operator "fix" a misconfiguration into an authorization bypass.
//
// Codex/ChatGPT subscription sign-in is a different credential and is not handled
// here. This is OpenAI Platform API service-account federation only.
package ociwif

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Issuer is the global OCI identity issuer for IDCS-issued access tokens.
const Issuer = "https://identity.oraclecloud.com/"

// OpenAIExchangeEndpoint is the only allowed federation endpoint.
const OpenAIExchangeEndpoint = "https://auth.openai.com/oauth/token"

// SubjectTypeInstance is the expected sub_type of an instance-principal token.
const SubjectTypeInstance = "instance"

var (
	ErrMissingComponent    = errors.New("ociwif requires a signer and an exchanger")
	ErrSourceUnavailable   = errors.New("ociwif could not obtain an OCI subject token")
	ErrWrongIssuer         = errors.New("ociwif subject token issuer is not the configured OCI issuer")
	ErrWrongAudience       = errors.New("ociwif subject token audience is not the configured identity domain")
	ErrWrongSubjectType    = errors.New("ociwif subject token is not an instance principal")
	ErrWrongInstance       = errors.New("ociwif subject token instance is not allowed")
	ErrWrongCompartment    = errors.New("ociwif subject token compartment is not allowed")
	ErrWrongDomain         = errors.New("ociwif subject token identity domain does not match")
	ErrWrongTenancy        = errors.New("ociwif subject token tenancy does not match")
	ErrSourceExpired       = errors.New("ociwif subject token is expired")
	ErrSourceLifetime      = errors.New("ociwif subject token lifetime is outside policy")
	ErrEndpointMutation    = errors.New("ociwif exchange endpoint is not the OpenAI federation endpoint")
	ErrMappingMismatch     = errors.New("ociwif exchange mapping does not match the configured provider")
	ErrExpiryInconsistency = errors.New("ociwif response expiry is inconsistent")
	ErrExchangeUnknown     = errors.New("ociwif exchange result is unknown and must be reconciled")
)

// Claims is the subset of OCI access-token claims this package needs. Values are
// supplied by a trusted signer adapter; this package does not decode untrusted
// JWTs by itself.
type Claims struct {
	Issuer          string
	Audience        []string
	SubjectType     string
	InstanceOCID    string
	CompartmentOCID string
	DomainID        string
	TenancyOCID     string
	IssuedAt        time.Time
	ExpiresAt       time.Time
}

// SourceToken carries the subject token and its parsed claims. Raw is never
// printed: Format replaces the whole value.
type SourceToken struct {
	Raw    string
	Claims Claims
}

func (SourceToken) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[redacted OCI subject token]")
}

// Signer obtains a subject token from the OCI instance metadata service and the
// instance-principal signer. Implementations live in the host, not in tests.
type Signer interface {
	SubjectToken(ctx context.Context) (SourceToken, error)
}

// ExchangeRequest is the exact federation request. The endpoint is not a field:
// it is fixed by OpenAIExchangeEndpoint so a caller cannot redirect the exchange.
type ExchangeRequest struct {
	SourceToken      string
	IdentityProvider string
	ServiceAccount   string
	Audience         string
}

func (ExchangeRequest) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[redacted ociwif exchange request]")
}

// TokenResponse is the raw federation response.
type TokenResponse struct {
	AccessToken     string
	TokenType       string
	IssuedTokenType string
	ExpiresIn       int64
	ExpiresAt       int64
}

func (TokenResponse) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "[redacted WIF response]") }

// Exchanger performs the OpenAI federation call.
type Exchanger interface {
	Exchange(ctx context.Context, req ExchangeRequest) (TokenResponse, error)
}

// Policy is the pinned expectation. Every field is required except the
// compartment list, which is an alternative to an instance list.
type Policy struct {
	Issuer            string
	Audience          string
	TenancyOCID       string
	DomainID          string
	Instances         []string
	Compartments      []string
	IdentityProvider  string
	ServiceAccount    string
	MaxSourceLifetime time.Duration
	MaxTokenLifetime  time.Duration
	MinTokenLifetime  time.Duration
	Now               func() time.Time
}

// Credential is a short-lived OpenAI access token. It is intentionally opaque.
type Credential struct {
	token   string
	expires time.Time
	clock   func() time.Time
}

func (Credential) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[redacted OpenAI WIF credential]")
}

// ExpiresAt reports when the credential must not be used any more.
func (c Credential) ExpiresAt() time.Time { return c.expires }

// Authorize attaches the token to the public OpenAI API origin only.
func (c Credential) Authorize(req *http.Request) error {
	if req == nil || req.URL == nil || req.URL.Scheme != "https" || req.URL.Host != "api.openai.com" ||
		req.URL.User != nil || req.URL.Opaque != "" || req.Header == nil || (req.Host != "" && req.Host != req.URL.Host) ||
		req.Header.Get("Authorization") != "" || c.token == "" || !c.now().Before(c.expires) {
		return errors.New("WIF credential cannot authorize this request")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return nil
}

func (c Credential) now() time.Time {
	if c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

// ValidateExchangeEndpoint rejects anything except the pinned OpenAI endpoint.
func ValidateExchangeEndpoint(endpoint string) error {
	if endpoint != OpenAIExchangeEndpoint {
		return ErrEndpointMutation
	}
	return nil
}

// Acquire verifies the subject token, exchanges it once and returns a credential.
// It never retries: an unknown exchange result must be reconciled by the caller.
func (p Policy) Acquire(ctx context.Context, signer Signer, exchanger Exchanger) (Credential, error) {
	if signer == nil || exchanger == nil {
		return Credential{}, ErrMissingComponent
	}
	if ctx == nil || ctx.Err() != nil {
		return Credential{}, ErrSourceUnavailable
	}
	if p.IdentityProvider == "" || p.ServiceAccount == "" {
		// Do not call the provider with an incomplete mapping.
		return Credential{}, ErrMappingMismatch
	}
	source, err := signer.SubjectToken(ctx)
	if err != nil || source.Raw == "" || ctx.Err() != nil {
		return Credential{}, ErrSourceUnavailable
	}
	clock := p.Now
	if clock == nil {
		clock = time.Now
	}
	started := clock()
	if err := p.verifyClaims(source.Claims, started); err != nil {
		return Credential{}, err
	}
	response, err := exchanger.Exchange(ctx, ExchangeRequest{
		SourceToken:      source.Raw,
		IdentityProvider: p.IdentityProvider,
		ServiceAccount:   p.ServiceAccount,
		Audience:         p.Audience,
	})
	if err != nil || ctx.Err() != nil {
		return Credential{}, ErrExchangeUnknown
	}
	finished := clock()
	if !finished.Before(source.Claims.ExpiresAt) {
		return Credential{}, ErrSourceExpired
	}
	return p.credential(response, started, finished, source.Claims.ExpiresAt)
}

func (p Policy) verifyClaims(c Claims, now time.Time) error {
	if c.Issuer != p.Issuer {
		return ErrWrongIssuer
	}
	if !contains(c.Audience, p.Audience) {
		return ErrWrongAudience
	}
	if c.SubjectType != SubjectTypeInstance {
		return ErrWrongSubjectType
	}
	if len(p.Instances) > 0 && !contains(p.Instances, c.InstanceOCID) {
		return ErrWrongInstance
	}
	if len(p.Instances) == 0 && len(p.Compartments) > 0 && !contains(p.Compartments, c.CompartmentOCID) {
		return ErrWrongCompartment
	}
	if p.DomainID != "" && c.DomainID != p.DomainID {
		return ErrWrongDomain
	}
	if c.TenancyOCID != p.TenancyOCID || c.TenancyOCID == "" {
		return ErrWrongTenancy
	}
	if !now.Before(c.ExpiresAt) {
		return ErrSourceExpired
	}
	lifetime := c.ExpiresAt.Sub(c.IssuedAt)
	if c.IssuedAt.IsZero() || c.IssuedAt.After(now) || lifetime <= 0 ||
		(p.MaxSourceLifetime > 0 && lifetime > p.MaxSourceLifetime) {
		return ErrSourceLifetime
	}
	return nil
}

func (p Policy) credential(r TokenResponse, started, finished, sourceExpiry time.Time) (Credential, error) {
	if !validBearerToken(r.AccessToken) || p.IdentityProvider == "" || p.ServiceAccount == "" {
		return Credential{}, ErrMappingMismatch
	}

	maxLifetime := time.Hour // OpenAI's documented maximum.
	if p.MaxTokenLifetime > 0 && p.MaxTokenLifetime < maxLifetime {
		maxLifetime = p.MaxTokenLifetime
	}
	if finished.Before(started) || r.ExpiresAt < 0 || r.ExpiresIn < 0 || r.ExpiresIn > int64(maxLifetime/time.Second) {
		return Credential{}, ErrExpiryInconsistency
	}

	var expires time.Time
	switch {
	case r.ExpiresAt > 0:
		expires = time.Unix(r.ExpiresAt, 0)
		if r.ExpiresIn > 0 {
			// The absolute timestamp comes from the token's exp. The
			// relative value is measured from issuance, which may occur
			// at any point during the request. Allow second rounding.
			issued := expires.Add(-time.Duration(r.ExpiresIn) * time.Second)
			if issued.Before(started.Add(-time.Second)) || issued.After(finished.Add(time.Second)) {
				return Credential{}, ErrExpiryInconsistency
			}
		} else if expires.Sub(started) > maxLifetime {
			return Credential{}, ErrExpiryInconsistency
		}
	case r.ExpiresIn > 0:
		// Without the absolute timestamp, use request start so time
		// spent receiving the response cannot extend the lifetime.
		expires = started.Add(time.Duration(r.ExpiresIn) * time.Second)
	default:
		return Credential{}, ErrExpiryInconsistency
	}

	if expires.Sub(finished) > maxLifetime {
		return Credential{}, ErrExpiryInconsistency
	}
	if sourceExpiry.Before(expires) {
		expires = sourceExpiry
	}
	if !expires.After(finished) || (p.MinTokenLifetime > 0 && expires.Sub(finished) < p.MinTokenLifetime) {
		return Credential{}, ErrExpiryInconsistency
	}
	return Credential{token: r.AccessToken, expires: expires, clock: p.Now}, nil
}

func isBearer(tokenType string) bool { return strings.EqualFold(tokenType, "Bearer") }

func validBearerToken(token string) bool {
	// RFC 6750 b64token; reject spaces, control characters, and punctuation
	// that could change how the Authorization header is parsed.
	base := strings.TrimRight(token, "=")
	if base == "" {
		return false
	}
	for i := 0; i < len(base); i++ {
		c := base[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' || c == '+' || c == '/' {
			continue
		}
		return false
	}
	return true
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
