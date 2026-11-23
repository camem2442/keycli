// Package discoveryfetch performs the one network step RFC 8414 metadata needs.
//
// The parsing package (internal/auth/oauth) deliberately holds no network code: its
// contract is that the trust decision is testable offline. This package is the other
// half — it fetches the document and hands the bytes to oauth.ParseDiscovery — and it
// exists separately so that purity boundary stays visible in the import graph.
//
// It does not cache, retry, or follow redirects. Those are policy decisions a caller may
// add later, but a client that silently retries or follows a redirect has delegated its
// issuer pin to whoever controls the first response.
package discoveryfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/auth/oauth"
)

var (
	// ErrTransport means the request could not be completed.
	ErrTransport = errors.New("discoveryfetch: request failed")
	// ErrRedirect means the response did not come from the pinned issuer's own URL.
	ErrRedirect = errors.New("discoveryfetch: redirect refused")
	// ErrStatus means the response was not a 200.
	ErrStatus = errors.New("discoveryfetch: unexpected status")
	// ErrContentType means the response was not JSON.
	ErrContentType = errors.New("discoveryfetch: unexpected content type")
	// ErrTooLarge means the body exceeded the configured bound.
	ErrTooLarge = errors.New("discoveryfetch: document too large")
)

// Doer is the part of *http.Client this package needs. Accepting an interface keeps the
// tests offline and lets callers supply their own transport (proxy, tracing, mTLS).
type Doer interface {
	Do(request *http.Request) (*http.Response, error)
}

// Policy bounds a single fetch. The zero value is usable: it applies the defaults below.
type Policy struct {
	// Timeout bounds the whole exchange. Zero means DefaultTimeout; a negative value
	// disables the extra bound and leaves the deadline to ctx.
	Timeout time.Duration
	// MaxBytes bounds the response body. Zero means oauth.MaxDiscoveryBytes.
	MaxBytes int64
	// UserAgent is sent only when non-empty.
	UserAgent string
}

// DefaultTimeout bounds a fetch when Policy.Timeout is zero.
const DefaultTimeout = 10 * time.Second

// Fetch retrieves and validates the metadata document for a pinned issuer.
//
// The issuer is the trust anchor: it is passed through to oauth.DiscoveryURL and to
// oauth.ParseDiscovery, so a document that describes another issuer is refused even when
// it arrived from the expected URL.
func Fetch(ctx context.Context, doer Doer, issuer string, policy Policy) (oauth.Discovery, error) {
	if doer == nil {
		return oauth.Discovery{}, fmt.Errorf("%w: nil doer", ErrTransport)
	}
	target, err := oauth.DiscoveryURL(issuer)
	if err != nil {
		return oauth.Discovery{}, err
	}
	maxBytes := policy.MaxBytes
	if maxBytes <= 0 {
		maxBytes = oauth.MaxDiscoveryBytes
	}
	// Read one byte past the bound so "too large" is distinguishable from "exactly at the
	// bound". Only increment when that cannot overflow int64.
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

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return oauth.Discovery{}, fmt.Errorf("%w: %w", ErrTransport, err)
	}
	// RFC 8414 section 3.1 is a plain GET of a well-known location. No credentials are
	// sent: the metadata is public, and an Authorization header here would leak a bearer
	// token to whichever host the issuer named.
	request.Header.Set("Accept", "application/json")
	if policy.UserAgent != "" {
		request.Header.Set("User-Agent", policy.UserAgent)
	}

	response, err := doer.Do(request)
	if err != nil {
		// A context deadline is worth distinguishing from a generic transport failure.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return oauth.Discovery{}, fmt.Errorf("%w: %w", ErrTransport, ctxErr)
		}
		return oauth.Discovery{}, fmt.Errorf("%w: %w", ErrTransport, err)
	}
	if response == nil {
		return oauth.Discovery{}, fmt.Errorf("%w: nil response", ErrTransport)
	}
	if response.Body == nil {
		return oauth.Discovery{}, fmt.Errorf("%w: nil body", ErrTransport)
	}
	defer response.Body.Close()

	// Doers such as *http.Client follow redirects by default. Comparing the URL that
	// actually produced the response against the one we asked for turns a redirect into
	// a refusal instead of a silent change of issuer.
	if err := requireSameURL(target, response.Request); err != nil {
		return oauth.Discovery{}, err
	}
	if response.StatusCode != http.StatusOK {
		return oauth.Discovery{}, fmt.Errorf("%w: %d", ErrStatus, response.StatusCode)
	}
	if err := requireJSON(response.Header.Get("Content-Type")); err != nil {
		return oauth.Discovery{}, err
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, readLimit))
	if err != nil {
		return oauth.Discovery{}, fmt.Errorf("%w: %w", ErrTransport, err)
	}
	if int64(len(body)) > maxBytes {
		return oauth.Discovery{}, fmt.Errorf("%w: more than %d bytes", ErrTooLarge, maxBytes)
	}
	return oauth.ParseDiscovery(body, issuer)
}

func requireSameURL(target string, request *http.Request) error {
	if request == nil || !oauth.SameEndpoint(target, request.URL) {
		// A doer that does not say where the response came from cannot prove the pin.
		return fmt.Errorf("%w: response did not come from %s", ErrRedirect, target)
	}
	return nil
}

// requireJSON is a policy check, not an RFC 8414 requirement: the RFC does not fix a
// media type, but refusing anything that is not JSON stops a login page served in place
// of the metadata from being parsed as if it were metadata.
//
// It accepts application/json and the structured-suffix form RFC 6839 defines
// (for example application/oauth-metadata+json). Anything else — notably text/html from a
// login page that answered instead of the metadata endpoint — is refused before parsing.
func requireJSON(contentType string) error {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrContentType, contentType)
	}
	if mediaType == "application/json" {
		return nil
	}
	if strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json") {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrContentType, contentType)
}
