// Package tokenexchange posts an RFC 6749 token request and parses the answer.
//
// The request builder and response parser live in internal/auth/oauth and are pure. This
// package adds the one step that needs a network: POST a form to the pinned token
// endpoint. It does not cache, retry, or follow redirects, and it never puts request or
// response bodies into errors.
package tokenexchange

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/auth/oauth"
)

var (
	// ErrRequest means the built exchange form exceeds MaxFormBytes. Endpoint problems
	// are reported as oauth.ErrExchange by the builder instead.
	ErrRequest = errors.New("tokenexchange: unusable request")
	// ErrTransport means the request could not be completed.
	ErrTransport = errors.New("tokenexchange: request failed")
	// ErrRedirect means the response did not come from the pinned token endpoint.
	ErrRedirect = errors.New("tokenexchange: redirect refused")
	// ErrStatus means the response was not a 200 and did not carry a usable OAuth error.
	// A classified OAuth error is wrapped together with this one, so callers can check
	// either.
	ErrStatus = errors.New("tokenexchange: unexpected status")
	// ErrContentType means a 200 response was not JSON.
	ErrContentType = errors.New("tokenexchange: unexpected content type")
	// ErrTooLarge means the body exceeded the configured bound.
	ErrTooLarge = errors.New("tokenexchange: response too large")
)

// Doer is the part of *http.Client this package needs.
type Doer interface {
	Do(request *http.Request) (*http.Response, error)
}

// Policy bounds a single exchange. The zero value is usable.
type Policy struct {
	// Timeout bounds the whole exchange. Zero means DefaultTimeout; a negative value
	// leaves the deadline to ctx.
	Timeout time.Duration
	// MaxBytes bounds the response body. Zero means oauth.MaxTokenResponseBytes.
	MaxBytes int64
	// UserAgent is sent only when non-empty.
	UserAgent string
}

// DefaultTimeout bounds an exchange when Policy.Timeout is zero.
const DefaultTimeout = 10 * time.Second

// MaxFormBytes bounds the encoded request body. The exchange form is a few hundred
// bytes; anything larger is a bug, not a configuration.
const MaxFormBytes = 16 << 10

// Exchange builds the code exchange form from the same Pending that Begin returned,
// posts it to provider.TokenEndpoint, and returns the credential set on success.
//
// Taking the provider, client and pending rather than a form and a URL is deliberate: a
// separate form argument could be built for one provider and posted to another, which is
// exactly the mismatch that leaks a verifier to the wrong host.
//
// A non-200 response is never allowed to yield credentials, even if its body happens to
// contain an access_token: only status 200 is treated as a success document.
func Exchange(ctx context.Context, doer Doer, provider oauth.Provider, client oauth.Client, pending oauth.Pending, code string, now time.Time, policy Policy) (oauth.Tokens, error) {
	if doer == nil {
		return oauth.Tokens{}, fmt.Errorf("%w: nil doer", ErrTransport)
	}
	form, err := oauth.BuildExchange(provider, client, pending, code)
	if err != nil {
		return oauth.Tokens{}, err
	}
	endpoint := provider.TokenEndpoint
	encoded := form.Encode()
	if len(encoded) > MaxFormBytes {
		return oauth.Tokens{}, fmt.Errorf("%w: form is %d bytes", ErrRequest, len(encoded))
	}

	maxBytes := policy.MaxBytes
	if maxBytes <= 0 {
		maxBytes = oauth.MaxTokenResponseBytes
	}
	// Read one byte past the bound so the error is precise; skip the increment when it
	// would overflow.
	readLimit := maxBytes
	if readLimit < math.MaxInt64 {
		readLimit++
	}

	timeout := policy.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(encoded))
	if err != nil {
		return oauth.Tokens{}, fmt.Errorf("%w: %w", ErrRequest, err)
	}
	// RFC 6749 section 4.1.3: form-encoded POST. No Authorization header: a public client
	// has no secret, and sending one here would defeat the reason PKCE exists.
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if policy.UserAgent != "" {
		request.Header.Set("User-Agent", policy.UserAgent)
	}

	response, err := doer.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return oauth.Tokens{}, fmt.Errorf("%w: %w", ErrTransport, ctxErr)
		}
		return oauth.Tokens{}, fmt.Errorf("%w: %w", ErrTransport, err)
	}
	if response == nil || response.Body == nil {
		return oauth.Tokens{}, fmt.Errorf("%w: nil response", ErrTransport)
	}
	defer response.Body.Close()

	if response.Request == nil || !oauth.SameEndpoint(endpoint, response.Request.URL) {
		return oauth.Tokens{}, fmt.Errorf("%w: response did not come from %s", ErrRedirect, endpoint)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, readLimit))
	if err != nil {
		return oauth.Tokens{}, fmt.Errorf("%w: %w", ErrTransport, err)
	}
	oversized := int64(len(body)) > maxBytes

	if response.StatusCode != http.StatusOK {
		// RFC 6749 section 5.2 returns 400 with a JSON error document. Classify it, but
		// keep ErrStatus so a caller can react to the status itself. An oversized error
		// body must not replace that with a truncation error.
		if oversized {
			return oauth.Tokens{}, fmt.Errorf("%w: %d", ErrStatus, response.StatusCode)
		}
		if classified := oauth.ParseTokenError(body); classified != nil && !errors.Is(classified, oauth.ErrTokenResponse) {
			return oauth.Tokens{}, fmt.Errorf("%w: %d: %w", ErrStatus, response.StatusCode, classified)
		}
		return oauth.Tokens{}, fmt.Errorf("%w: %d", ErrStatus, response.StatusCode)
	}
	if oversized {
		return oauth.Tokens{}, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, maxBytes)
	}
	if !oauth.IsJSONMediaType(response.Header.Get("Content-Type")) {
		return oauth.Tokens{}, fmt.Errorf("%w: %q", ErrContentType, response.Header.Get("Content-Type"))
	}
	return oauth.ParseTokenResponse(body, now)
}
