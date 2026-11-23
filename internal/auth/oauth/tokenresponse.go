// RFC 6749 section 5 token response parsing.
//
// Pure: no clock, no network, no storage. The caller supplies the current time so an
// expiry becomes an absolute instant; ParseTokenResponse never reads the wall clock.
package oauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrTokenResponse means the bytes are not a usable RFC 6749 token response.
	ErrTokenResponse = errors.New("oauth: unusable token response")
	// ErrTokenRejected means the authorization server refused the grant. Retrying the
	// same request cannot help; the caller must start a new interactive authorization.
	ErrTokenRejected = errors.New("oauth: authorization server rejected the request")
	// ErrTokenTransient means the server reported a temporary failure. A later attempt
	// may succeed, but this package does not schedule one.
	ErrTokenTransient = errors.New("oauth: authorization server reported a temporary failure")
)

// MaxTokenResponseBytes bounds the response. Token responses are small; a larger body is
// a mistake or an attempt to make the parser work for it.
const MaxTokenResponseBytes = 64 << 10

// maxErrorCodeBytes bounds the error member before it is echoed. RFC 6749 codes are a
// dozen characters; anything larger is not something to copy into a log line.
const maxErrorCodeBytes = 64

// MaxTokenBytes bounds an individual credential. Tokens are opaque but short enough that
// a megabyte value is never legitimate.
const MaxTokenBytes = 8192

// MaxExpiresInSeconds bounds expires_in at one year. Without a bound, a server could
// hand out an expiry far enough in the future to defeat the skew policy.
const MaxExpiresInSeconds = 366 * 24 * 60 * 60

// terminalErrors are the RFC 6749 section 5.2 codes that mean "do not retry this grant".
// invalid_client and unauthorized_client are included because a public client that is
// refused will not start working on the next call.
var terminalErrors = map[string]struct{}{
	"invalid_request":        {},
	"invalid_client":         {},
	"invalid_grant":          {},
	"unauthorized_client":    {},
	"unsupported_grant_type": {},
	"invalid_scope":          {},
	"access_denied":          {},
}

var transientErrors = map[string]struct{}{
	"server_error":            {},
	"temporarily_unavailable": {},
}

// ParseTokenResponse turns a success or error document into a Tokens value or a typed
// error. The caller must keep the returned value out of logs and receipts; Tokens
// refuses to marshal for exactly that reason.
func ParseTokenResponse(raw []byte, now time.Time) (Tokens, error) {
	if len(raw) == 0 || len(raw) > MaxTokenResponseBytes {
		return Tokens{}, ErrTokenResponse
	}
	members, err := decodeTokenMembers(raw)
	if err != nil {
		return Tokens{}, err
	}

	if rawError, present := members["error"]; present {
		return Tokens{}, classifyTokenError(rawError, members)
	}

	accessToken, err := requiredString(members, "access_token")
	if err != nil || !safeToken(accessToken) || len(accessToken) > MaxTokenBytes {
		return Tokens{}, ErrTokenResponse
	}

	tokenType := ""
	if rawType, present := members["token_type"]; present {
		value, err := decodeMemberString(rawType)
		if err != nil {
			return Tokens{}, ErrTokenResponse
		}
		// RFC 6749 makes token_type REQUIRED. This client only sends bearer tokens, and
		// Plan already treats an omitted type as acceptable, so an empty value is
		// tolerated; anything else is refused rather than stored and ignored.
		if value != "" && !strings.EqualFold(value, "Bearer") {
			return Tokens{}, ErrTokenResponse
		}
		tokenType = value
	}

	refreshToken := ""
	if rawRefresh, present := members["refresh_token"]; present {
		value, err := decodeMemberString(rawRefresh)
		if err != nil || (value != "" && (!safeToken(value) || len(value) > MaxTokenBytes)) {
			return Tokens{}, ErrTokenResponse
		}
		refreshToken = value
	}

	expiresAt := time.Time{}
	if rawExpires, present := members["expires_in"]; present {
		seconds, err := decodeExpiresIn(rawExpires)
		if err != nil {
			return Tokens{}, err
		}
		// A zero-second lifetime is never useful: the value would be stale before the
		// caller could use it, and Plan would demand a refresh immediately.
		if seconds <= 0 || seconds > MaxExpiresInSeconds {
			return Tokens{}, ErrTokenResponse
		}
		expiresAt = now.Add(time.Duration(seconds) * time.Second)
	}

	scope := ""
	if rawScope, present := members["scope"]; present {
		value, err := decodeMemberString(rawScope)
		if err != nil {
			return Tokens{}, ErrTokenResponse
		}
		// RFC 6749 section 3.3 separates scope-tokens with a single space. Splitting with
		// strings.Fields would accept a tab or newline and then store the raw value with
		// that byte in it, so the raw string is checked before any splitting happens.
		for i := 0; i < len(value); i++ {
			ch := value[i]
			if ch == ' ' {
				continue
			}
			if !validScopeToken(string(ch)) {
				return Tokens{}, ErrTokenResponse
			}
		}
		for _, token := range scopes(value) {
			if !validScopeToken(token) {
				return Tokens{}, ErrTokenResponse
			}
		}
		scope = value
	}

	return Tokens{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
		ExpiresAt:    expiresAt,
		Scope:        scope,
	}, nil
}

// ParseTokenError reads only an RFC 6749 section 5.2 error document. It is what a caller
// uses for a non-200 response, where accepting a credential would be wrong even if the
// body happened to contain one; a success document therefore yields ErrTokenResponse.
func ParseTokenError(raw []byte) error {
	if len(raw) == 0 || len(raw) > MaxTokenResponseBytes {
		return ErrTokenResponse
	}
	members, err := decodeTokenMembers(raw)
	if err != nil {
		return err
	}
	rawError, present := members["error"]
	if !present {
		return ErrTokenResponse
	}
	return classifyTokenError(rawError, members)
}

func decodeTokenMembers(raw []byte) (map[string]json.RawMessage, error) {
	if err := rejectDuplicateKeys(raw); err != nil {
		return nil, ErrTokenResponse
	}
	var members map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&members); err != nil || members == nil {
		return nil, ErrTokenResponse
	}
	return members, nil
}

// classifyTokenError maps an RFC 6749 section 5.2 error document onto the three outcomes
// a caller can act on. The description is deliberately not included: it is server text,
// and echoing it into logs is how a token endpoint ends up in a receipt.
func classifyTokenError(rawError json.RawMessage, members map[string]json.RawMessage) error {
	code, err := decodeMemberString(rawError)
	if err != nil || code == "" {
		return ErrTokenResponse
	}
	// The code is server text that ends up in an error string, so it must be a short
	// token before it is interpolated. Otherwise a body could put CR/LF or a very long
	// string into a log line that the doc promises contains only a code.
	if len(code) > maxErrorCodeBytes || !validScopeToken(code) {
		return ErrTokenResponse
	}
	if _, ambiguous := members["access_token"]; ambiguous {
		// A document that both fails and carries a credential cannot be trusted either way.
		return ErrTokenResponse
	}
	if _, ok := terminalErrors[code]; ok {
		return fmt.Errorf("%w: %q", ErrTokenRejected, code)
	}
	if _, ok := transientErrors[code]; ok {
		return fmt.Errorf("%w: %q", ErrTokenTransient, code)
	}
	return fmt.Errorf("%w: %q", ErrTokenResponse, code)
}

func requiredString(members map[string]json.RawMessage, name string) (string, error) {
	raw, present := members[name]
	if !present {
		return "", ErrTokenResponse
	}
	return decodeMemberString(raw)
}

func decodeMemberString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", ErrTokenResponse
	}
	return value, nil
}

// decodeExpiresIn accepts an integer number only. A JSON string or float is refused:
// providers that publish "3600" as a string are unusual, and silently coercing it would
// hide a document that differs from the RFC.
func decodeExpiresIn(raw json.RawMessage) (int64, error) {
	text := strings.TrimSpace(string(raw))
	if text == "" || strings.ContainsAny(text, ".eE") {
		return 0, ErrTokenResponse
	}
	seconds, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, ErrTokenResponse
	}
	return seconds, nil
}
