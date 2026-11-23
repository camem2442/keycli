package discoveryfetch

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/auth/oauth"
)

const (
	pinned    = "https://access.example.com"
	wellKnown = pinned + "/.well-known/oauth-authorization-server"
)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

func metadata(overrides map[string]string) string {
	fields := map[string]string{
		"issuer":                                `"` + pinned + `"`,
		"authorization_endpoint":                `"` + pinned + `/a"`,
		"token_endpoint":                        `"` + pinned + `/t"`,
		"response_types_supported":              `["code"]`,
		"code_challenge_methods_supported":      `["S256"]`,
		"token_endpoint_auth_methods_supported": `["none"]`,
	}
	for key, value := range overrides {
		if value == "" {
			delete(fields, key)
			continue
		}
		fields[key] = value
	}
	parts := make([]string, 0, len(fields))
	for key, value := range fields {
		parts = append(parts, `"`+key+`":`+value)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// reply builds a response that claims to come from request's own URL, the way a real
// client does after a non-redirected exchange.
func reply(request *http.Request, status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func accept(body string) doerFunc {
	return func(request *http.Request) (*http.Response, error) {
		return reply(request, http.StatusOK, "application/json", body), nil
	}
}

func TestFetchReturnsParsedMetadata(t *testing.T) {
	var seen *http.Request
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		seen = request
		return reply(request, http.StatusOK, "application/json", metadata(nil)), nil
	})

	discovery, err := Fetch(context.Background(), doer, pinned, Policy{UserAgent: "keycli/test"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if discovery.Issuer != pinned || discovery.TokenEndpoint != pinned+"/t" {
		t.Fatalf("wrong metadata: %+v", discovery)
	}
	if seen.Method != http.MethodGet {
		t.Fatalf("want GET, got %s", seen.Method)
	}
	if seen.URL.String() != wellKnown {
		t.Fatalf("wrong URL: %s", seen.URL)
	}
	if got := seen.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("want Accept application/json, got %q", got)
	}
	// The metadata is public. Sending credentials here would hand a bearer token to
	// whatever host the pinned issuer names.
	if got := seen.Header.Get("Authorization"); got != "" {
		t.Fatalf("must not send Authorization, got %q", got)
	}
	if got := seen.Header.Get("User-Agent"); got != "keycli/test" {
		t.Fatalf("want the configured User-Agent, got %q", got)
	}
}

func TestFetchOmitsEmptyUserAgent(t *testing.T) {
	var seen *http.Request
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		seen = request
		return reply(request, http.StatusOK, "application/json", metadata(nil)), nil
	})
	if _, err := Fetch(context.Background(), doer, pinned, Policy{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, present := seen.Header["User-Agent"]; present {
		t.Fatalf("User-Agent must be absent when not configured")
	}
}

func TestFetchRefusesRedirectedResponse(t *testing.T) {
	// Each case isolates one component of the comparison. A weakened sameURL that only
	// checked the host, or only the scheme and host, would pass the cross-host case above
	// and still miss these.
	cases := map[string]string{
		"other host":       "https://evil.example",
		"same host, path":  pinned + "/other",
		"scheme downgrade": "http://access.example.com",
		"port change":      "https://access.example.com:8443",
	}
	for name, destination := range cases {
		t.Run(name, func(t *testing.T) {
			doer := doerFunc(func(request *http.Request) (*http.Response, error) {
				response := reply(request, http.StatusOK, "application/json", metadata(nil))
				response.Request = &http.Request{URL: mustParse(t, destination+request.URL.Path)}
				return response, nil
			})
			if _, err := Fetch(context.Background(), doer, pinned, Policy{}); !errors.Is(err, ErrRedirect) {
				t.Fatalf("want ErrRedirect, got %v", err)
			}
		})
	}
}

func TestFetchRefusesResponseWithoutProvenance(t *testing.T) {
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		response := reply(request, http.StatusOK, "application/json", metadata(nil))
		response.Request = nil
		return response, nil
	})
	if _, err := Fetch(context.Background(), doer, pinned, Policy{}); !errors.Is(err, ErrRedirect) {
		t.Fatalf("want ErrRedirect, got %v", err)
	}
}

func TestFetchRefusesStatusAndContentType(t *testing.T) {
	cases := map[string]struct {
		doer   Doer
		target error
	}{
		"404": {doerFunc(func(r *http.Request) (*http.Response, error) {
			return reply(r, http.StatusNotFound, "application/json", `{}`), nil
		}), ErrStatus},
		"html login page": {doerFunc(func(r *http.Request) (*http.Response, error) {
			return reply(r, http.StatusOK, "text/html; charset=utf-8", "<html></html>"), nil
		}), ErrContentType},
		"missing content type": {doerFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(metadata(nil))), Request: r}, nil
		}), ErrContentType},
		"malformed content type": {doerFunc(func(r *http.Request) (*http.Response, error) {
			return reply(r, http.StatusOK, "application/json; charset", metadata(nil)), nil
		}), ErrContentType},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Fetch(context.Background(), test.doer, pinned, Policy{}); !errors.Is(err, test.target) {
				t.Fatalf("want %v, got %v", test.target, err)
			}
		})
	}
}

func TestFetchAcceptsStructuredJSONSuffix(t *testing.T) {
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		return reply(request, http.StatusOK, "application/oauth-metadata+json", metadata(nil)), nil
	})
	if _, err := Fetch(context.Background(), doer, pinned, Policy{}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

func TestFetchBoundsTheBody(t *testing.T) {
	body := metadata(nil)
	doer := accept(body)
	if _, err := Fetch(context.Background(), doer, pinned, Policy{MaxBytes: int64(len(body)) - 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	// Exactly at the bound is fine.
	if _, err := Fetch(context.Background(), doer, pinned, Policy{MaxBytes: int64(len(body))}); err != nil {
		t.Fatalf("exact bound must be accepted: %v", err)
	}
	// The largest bound must not overflow the read limit and read nothing.
	if _, err := Fetch(context.Background(), doer, pinned, Policy{MaxBytes: math.MaxInt64}); err != nil {
		t.Fatalf("MaxInt64 bound must be accepted: %v", err)
	}
}

func TestFetchRejectsADocumentForAnotherIssuer(t *testing.T) {
	body := metadata(map[string]string{"issuer": `"https://other.example"`})
	if _, err := Fetch(context.Background(), accept(body), pinned, Policy{}); !errors.Is(err, oauth.ErrDiscoveryIssuer) {
		t.Fatalf("want ErrDiscoveryIssuer, got %v", err)
	}
}

func TestFetchRejectsAnUntrustworthyIssuerBeforeDialing(t *testing.T) {
	called := false
	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not be called")
	})
	for _, issuer := range []string{"http://access.example.com", "https://user:pw@access.example.com", ""} {
		if _, err := Fetch(context.Background(), doer, issuer, Policy{}); !errors.Is(err, oauth.ErrDiscoveryIssuer) {
			t.Fatalf("%q: want ErrDiscoveryIssuer, got %v", issuer, err)
		}
	}
	if called {
		t.Fatalf("the request must not be attempted for an untrustworthy issuer")
	}
}

func TestFetchReportsTransportFailures(t *testing.T) {
	doer := doerFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("dial tcp: refused") })
	if _, err := Fetch(context.Background(), doer, pinned, Policy{}); !errors.Is(err, ErrTransport) {
		t.Fatalf("want ErrTransport, got %v", err)
	}
	if _, err := Fetch(context.Background(), nil, pinned, Policy{}); !errors.Is(err, ErrTransport) {
		t.Fatalf("nil doer: want ErrTransport, got %v", err)
	}
	for name, response := range map[string]*http.Response{
		"nil response": nil,
		"nil body":     {StatusCode: http.StatusOK, Header: http.Header{}, Body: nil},
	} {
		doer := doerFunc(func(*http.Request) (*http.Response, error) { return response, nil })
		if _, err := Fetch(context.Background(), doer, pinned, Policy{}); !errors.Is(err, ErrTransport) {
			t.Fatalf("%s: want ErrTransport, got %v", name, err)
		}
	}
}

func TestFetchHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})
	if _, err := Fetch(ctx, doer, pinned, Policy{}); !errors.Is(err, ErrTransport) {
		t.Fatalf("want ErrTransport, got %v", err)
	}
}

func TestFetchClosesTheBodyOnEveryPath(t *testing.T) {
	for name, response := range map[string]func(*http.Request) *http.Response{
		"success": func(r *http.Request) *http.Response {
			return reply(r, http.StatusOK, "application/json", metadata(nil))
		},
		"bad status": func(r *http.Request) *http.Response {
			return reply(r, http.StatusInternalServerError, "application/json", "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			closer := &trackingCloser{Reader: strings.NewReader("{}")}
			doer := doerFunc(func(request *http.Request) (*http.Response, error) {
				resp := response(request)
				resp.Body = closer
				return resp, nil
			})
			_, _ = Fetch(context.Background(), doer, pinned, Policy{})
			if !closer.closed {
				t.Fatalf("response body must be closed")
			}
		})
	}
}

// The default is part of the contract, not an implementation detail: loosening it would
// let a stalled metadata endpoint hold a login for much longer than documented. Asserting
// the constant is what stops a band check from accepting a larger value.
func TestDefaultTimeoutIsTenSeconds(t *testing.T) {
	if DefaultTimeout != 10*time.Second {
		t.Fatalf("DefaultTimeout is %v; the contract documents 10s", DefaultTimeout)
	}
}

func TestFetchSetsADeadlineFromPolicy(t *testing.T) {
	cases := map[string]struct {
		policy       Policy
		wantDeadline bool
	}{
		"zero means default": {Policy{}, true},
		"negative disables":  {Policy{Timeout: -time.Second}, false},
		"positive applies":   {Policy{Timeout: time.Minute}, true},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			var deadline time.Time
			var present bool
			doer := doerFunc(func(request *http.Request) (*http.Response, error) {
				deadline, present = request.Context().Deadline()
				return reply(request, http.StatusOK, "application/json", metadata(nil)), nil
			})
			if _, err := Fetch(context.Background(), doer, pinned, test.policy); err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if present != test.wantDeadline {
				t.Fatalf("deadline present=%v, want %v", present, test.wantDeadline)
			}
			if !present {
				return
			}
			// Pin the value, not just its existence: zero must mean DefaultTimeout, and a
			// positive policy must be the deadline. The lower band allows for elapsed time
			// between setting the deadline and reading it back.
			want := test.policy.Timeout
			if want == 0 {
				want = DefaultTimeout
			}
			if remaining := time.Until(deadline); remaining > want || remaining < want/2 {
				t.Fatalf("deadline %v is outside (%v/2, %v]", remaining, want, want)
			}
		})
	}
}

func TestFetchTimeoutInterruptsABodyRead(t *testing.T) {
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		response := reply(request, http.StatusOK, "application/json", "")
		response.Body = &blockingBody{ctx: request.Context()}
		return response, nil
	})
	_, err := Fetch(context.Background(), doer, pinned, Policy{Timeout: 20 * time.Millisecond})
	if !errors.Is(err, ErrTransport) {
		t.Fatalf("want ErrTransport, got %v", err)
	}
	// The cause must stay unwrappable so a caller can distinguish a timeout.
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want the deadline cause to survive wrapping, got %v", err)
	}
}

// blockingBody only returns when the request context ends, which is what a server that
// accepted the headers and then stalled on the body looks like.
type blockingBody struct{ ctx context.Context }

func (b *blockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *blockingBody) Close() error { return nil }

type trackingCloser struct {
	io.Reader
	closed bool
}

func (c *trackingCloser) Close() error { c.closed = true; return nil }

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("bad test URL %q: %v", raw, err)
	}
	return parsed
}
