package discoveryfetch

import (
	"context"
	"errors"
	"io"
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
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		response := reply(request, http.StatusOK, "application/json", metadata(nil))
		response.Request = &http.Request{URL: mustParse(t, "https://evil.example"+request.URL.Path)}
		return response, nil
	})
	if _, err := Fetch(context.Background(), doer, pinned, Policy{}); !errors.Is(err, ErrRedirect) {
		t.Fatalf("want ErrRedirect, got %v", err)
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

func TestPolicyTimeoutApplies(t *testing.T) {
	// A negative timeout disables the extra bound and leaves the caller's context alone.
	if _, err := Fetch(context.Background(), accept(metadata(nil)), pinned, Policy{Timeout: -time.Second}); err != nil {
		t.Fatalf("negative timeout must not fail: %v", err)
	}
	// A positive timeout must produce a cancelable context that ends the request.
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	if _, err := Fetch(context.Background(), doer, pinned, Policy{Timeout: 20 * time.Millisecond}); !errors.Is(err, ErrTransport) {
		t.Fatalf("want ErrTransport, got %v", err)
	}
}

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
