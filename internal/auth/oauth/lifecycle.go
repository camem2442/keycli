// Token lifecycle decisions for a native public client.
//
// Pure and side-effect-free, like package oauth's flow core: no network, no keychain,
// no filesystem, no clock. Callers pass the tokens they hold and the current time, and
// receive a decision they must then act on. Tokens are never logged or formatted.
package oauth

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrTokenSet means the supplied token material cannot be used at all.
	ErrTokenSet = errors.New("oauth: unusable token set")
)

// Tokens is secret-bearing. Callers must store it in a protected backend and must not
// serialize it into logs, receipts, or agent output.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
	Scope        string
}

// String and GoString keep a token set out of diagnostics by construction.
func (Tokens) String() string     { return "oauth.Tokens{credentials:redacted}" }
func (t Tokens) GoString() string { return t.String() }

// Action is what the caller must do next. It never implies a network call by itself.
type Action int

const (
	// UseAccessToken means the current access token is still valid beyond the skew.
	UseAccessToken Action = iota
	// Refresh means the access token is stale and a refresh token is available.
	Refresh
	// Reauthorize means only a new interactive login can continue.
	// A rejected refresh (invalid_grant) also lands here: the refresh token is dead.
	Reauthorize
)

func (a Action) String() string {
	switch a {
	case UseAccessToken:
		return "use_access_token"
	case Refresh:
		return "refresh"
	case Reauthorize:
		return "reauthorize"
	default:
		return "unknown"
	}
}

// Policy is the caller's tolerance. Skew is how early a token counts as stale, so a
// request is not sent with a token that expires while it is in flight.
type Policy struct {
	Skew time.Duration
}

// Plan reports the next action for an access token.
//
// Rules, in order:
//   - No access token at all: a refresh token alone is enough to Refresh, otherwise Reauthorize.
//   - A token type other than Bearer (case-insensitive) is refused: this client only sends bearer.
//   - A zero expiry is refused rather than treated as "never expires"; servers must say when.
//   - Expired (or within Skew of expiry): Refresh when a refresh token exists, else Reauthorize.
func Plan(t Tokens, now time.Time, policy Policy) (Action, error) {
	if strings.ContainsAny(t.AccessToken, " \t\r\n") {
		return Reauthorize, ErrTokenSet
	}
	if kind := strings.TrimSpace(t.TokenType); kind != "" && !strings.EqualFold(kind, "Bearer") {
		return Reauthorize, ErrTokenSet
	}
	if t.AccessToken == "" {
		if t.RefreshToken != "" {
			return Refresh, nil
		}
		return Reauthorize, nil
	}
	if t.ExpiresAt.IsZero() {
		return Reauthorize, ErrTokenSet
	}
	if now.Add(policy.Skew).Before(t.ExpiresAt) {
		return UseAccessToken, nil
	}
	if t.RefreshToken != "" {
		return Refresh, nil
	}
	return Reauthorize, nil
}

// Rotation describes what a refresh response did to the refresh token.
type Rotation int

const (
	// RotationNone means the server returned no new refresh token; keep the old one.
	RotationNone Rotation = iota
	// RotationReplaced means the server issued a new refresh token; discard the old one.
	RotationReplaced
)

// ApplyRefresh folds a token-endpoint response into the stored set.
//
// It enforces the rules the lifecycle depends on:
//   - A refresh response must carry a new access token; otherwise nothing is adopted.
//   - An omitted refresh token means "unchanged", not "cleared", which is what
//     RFC 6749 section 6 permits.
//   - A response whose refresh token equals the old one is not rotation; adopting it as
//     such would hide a server that is not rotating and make reuse detection useless.
//   - Scope may only shrink; a refresh must not silently widen authority.
func ApplyRefresh(previous Tokens, next Tokens, now time.Time, policy Policy) (Tokens, Rotation, error) {
	if strings.TrimSpace(next.AccessToken) == "" {
		return previous, RotationNone, ErrTokenSet
	}
	if strings.ContainsAny(next.AccessToken, " \t\r\n") {
		return previous, RotationNone, ErrTokenSet
	}
	if kind := strings.TrimSpace(next.TokenType); kind != "" && !strings.EqualFold(kind, "Bearer") {
		return previous, RotationNone, ErrTokenSet
	}
	if next.ExpiresAt.IsZero() {
		return previous, RotationNone, ErrTokenSet
	}
	if !next.ExpiresAt.After(now) {
		return previous, RotationNone, ErrTokenSet
	}
	if widenedScope(previous.Scope, next.Scope) {
		return previous, RotationNone, ErrTokenSet
	}

	adopted := next
	rotation := RotationNone
	switch next.RefreshToken {
	case "":
		adopted.RefreshToken = previous.RefreshToken
	default:
		if next.RefreshToken != previous.RefreshToken {
			rotation = RotationReplaced
		}
	}
	if adopted.Scope == "" {
		adopted.Scope = previous.Scope
	}
	// Re-evaluate the adopted set so a caller cannot store something Plan would refuse.
	if _, err := Plan(adopted, now, policy); err != nil {
		return previous, RotationNone, err
	}
	return adopted, rotation, nil
}

// widenedScope reports whether next contains a scope token that previous did not grant.
func widenedScope(previous, next string) bool {
	if strings.TrimSpace(next) == "" {
		return false
	}
	held := map[string]struct{}{}
	for _, scope := range strings.Fields(previous) {
		held[scope] = struct{}{}
	}
	for _, scope := range strings.Fields(next) {
		if _, ok := held[scope]; !ok {
			return true
		}
	}
	return false
}

// Logout is what a sign-out must do with the material it holds. It carries no secrets
// and is deliberately a plan: this package performs no revocation request.
type Logout struct {
	// RevokeRefresh is true when an RFC 7009 revocation call is worthwhile.
	RevokeRefresh bool
	// ClearStored is always true: a sign-out that keeps tokens is not a sign-out.
	ClearStored bool
}

// PlanLogout decides whether a revocation request is worth sending before clearing.
//
// A refresh token is worth revoking; an access token is not, because revocation of an
// access token is not honoured by every provider and the value is short-lived anyway.
// Keeping the rule here stops callers from inventing their own.
func PlanLogout(t Tokens) Logout {
	return Logout{RevokeRefresh: t.RefreshToken != "", ClearStored: true}
}
