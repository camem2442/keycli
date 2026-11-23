package tokenexchange

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
	endpoint = "https://access.example.com/token"
	issuer   = "https://access.example.com"
)

var fixedNow = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

func successDocument() string {
	return `{"access_token":"at-1","token_type":"Bearer","expires_in":3600,"refresh_token":"rt-1"}`
}

func reply(request *http.Request, status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func testForm() url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {"the-code"},
		"redirect_uri":  {"com.example.app:/callback"},
		"client_id":     {"client-id"},
		"code_verifier": {strings.Repeat("v", 43)},
	}
}

func TestExchangeReturnsCredentials(t *testing.T) {
	var seen *http.Request
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		seen = request
		return reply(request, http.StatusOK, "application/json", successDocument()), nil
	})
	form := testForm()
	tokens, err := Exchange(context.Background(), doer, endpoint, form, fixedNow, Policy{UserAgent: "keycli/test"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if tokens.AccessToken != "at-1" || tokens.RefreshToken != "rt-1" {
		t.Fatalf("wrong credentials: %+v", tokens)
	}
	if want := fixedNow.Add(time.Hour); !tokens.ExpiresAt.Equal(want) {
		t.Fatalf("expiry: want %v, got %v", want, tokens.ExpiresAt)
	}
	if seen.Method != http.MethodPost {
		t.Fatalf("want POST, got %s", seen.Method)
	}
	if got := seen.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
		t.Fatalf("wrong content type: %q", got)
	}
	if got := seen.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("wrong accept: %q", got)
	}
	if got := seen.Header.Get("Authorization"); got != "" {
		t.Fatalf("public client must not send credentials, got %q", got)
	}
	body, err := io.ReadAll(seen.Body)
	if err != nil {
		t.Fatalf("reading sent body: %v", err)
	}
	sent, err := url.ParseQuery(string(body))
	if err != nil {
		t.Fatalf("sent body is not a form: %v", err)
	}
	for key := range form {
		if sent.Get(key) != form.Get(key) {
			t.Fatalf("%s was altered: %q", key, sent.Get(key))
		}
	}
}

func TestExchangeRefusesRedirectedResponse(t *testing.T) {
	for name, destination := range map[string]string{
		"other host":       "https://evil.example/token",
		"other path":       issuer + "/other",
		"scheme downgrade": "http://access.example.com/token",
	} {
		t.Run(name, func(t *testing.T) {
			doer := doerFunc(func(request *http.Request) (*http.Response, error) {
				response := reply(request, http.StatusOK, "application/json", successDocument())
				response.Request = &http.Request{URL: mustParse(t, destination)}
				return response, nil
			})
			if tokens, err := Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{}); !errors.Is(err, ErrRedirect) {
				t.Fatalf("want ErrRedirect, got tokens=%+v err=%v", tokens, err)
			}
		})
	}
}

func TestExchangeClassifiesErrorResponses(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		target error
	}{
		"invalid grant":   {http.StatusBadRequest, `{"error":"invalid_grant"}`, oauth.ErrTokenRejected},
		"server error":    {http.StatusInternalServerError, `{"error":"server_error"}`, oauth.ErrTokenTransient},
		"unknown code":    {http.StatusBadRequest, `{"error":"something_new"}`, nil},
		"html error page": {http.StatusBadGateway, `<html>bad gateway</html>`, nil},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			doer := doerFunc(func(request *http.Request) (*http.Response, error) {
				return reply(request, test.status, "application/json", test.body), nil
			})
			tokens, err := Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{})
			if !errors.Is(err, ErrStatus) {
				t.Fatalf("want ErrStatus, got %v", err)
			}
			if test.target != nil && !errors.Is(err, test.target) {
				t.Fatalf("want %v, got %v", test.target, err)
			}
			if tokens.AccessToken != "" {
				t.Fatalf("a non-200 response must never yield credentials")
			}
		})
	}
}

func TestExchangeNeverAcceptsCredentialsFromANon200(t *testing.T) {
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		// A server bug or an attack: status says failure, body looks like success.
		return reply(request, http.StatusBadRequest, "application/json", successDocument()), nil
	})
	if tokens, err := Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{}); !errors.Is(err, ErrStatus) || tokens.AccessToken != "" {
		t.Fatalf("want ErrStatus and no credentials, got tokens=%+v err=%v", tokens, err)
	}
}

func TestExchangeRequiresJSONOnSuccess(t *testing.T) {
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		return reply(request, http.StatusOK, "text/html", "<html></html>"), nil
	})
	if _, err := Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{}); !errors.Is(err, ErrContentType) {
		t.Fatalf("want ErrContentType, got %v", err)
	}
}

func TestExchangeBoundsTheResponse(t *testing.T) {
	body := successDocument()
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		return reply(request, http.StatusOK, "application/json", body), nil
	})
	if _, err := Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{MaxBytes: int64(len(body)) - 1}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestExchangeRefusesUnusableRequests(t *testing.T) {
	called := false
	doer := doerFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("must not be called")
	})
	for name, test := range map[string]struct {
		endpoint string
		form     url.Values
	}{
		"http endpoint":     {"http://access.example.com/token", testForm()},
		"userinfo endpoint": {"https://u:p@access.example.com/token", testForm()},
		"query endpoint":    {endpoint + "?x=1", testForm()},
		"empty form":        {endpoint, url.Values{}},
		"nil form":          {endpoint, nil},
		"oversized form":    {endpoint, url.Values{"code": {strings.Repeat("c", MaxFormBytes+1)}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Exchange(context.Background(), doer, test.endpoint, test.form, fixedNow, Policy{}); !errors.Is(err, ErrRequest) {
				t.Fatalf("want ErrRequest, got %v", err)
			}
		})
	}
	if called {
		t.Fatalf("an unusable request must not reach the network")
	}
}

func TestExchangeReportsTransportFailures(t *testing.T) {
	if _, err := Exchange(context.Background(), nil, endpoint, testForm(), fixedNow, Policy{}); !errors.Is(err, ErrTransport) {
		t.Fatalf("nil doer: want ErrTransport, got %v", err)
	}
	doer := doerFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("dial refused") })
	if _, err := Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{}); !errors.Is(err, ErrTransport) {
		t.Fatalf("want ErrTransport, got %v", err)
	}
}

func TestExchangeTimeoutInterruptsABodyRead(t *testing.T) {
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		response := reply(request, http.StatusOK, "application/json", "")
		response.Body = &blockingBody{ctx: request.Context()}
		return response, nil
	})
	_, err := Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{Timeout: 20 * time.Millisecond})
	if !errors.Is(err, ErrTransport) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want ErrTransport with the deadline cause, got %v", err)
	}
}

func TestExchangeErrorsDoNotContainTheForm(t *testing.T) {
	doer := doerFunc(func(request *http.Request) (*http.Response, error) {
		return reply(request, http.StatusBadRequest, "application/json", `{"error":"invalid_grant"}`), nil
	})
	_, err := Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{"the-code", strings.Repeat("v", 43), "client-id"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked request material: %v", err)
		}
	}
}

func TestExchangeClosesTheBody(t *testing.T) {
	for name, status := range map[string]int{"success": http.StatusOK, "failure": http.StatusBadRequest} {
		t.Run(name, func(t *testing.T) {
			closer := &trackingCloser{Reader: strings.NewReader(successDocument())}
			doer := doerFunc(func(request *http.Request) (*http.Response, error) {
				response := reply(request, status, "application/json", "")
				response.Body = closer
				return response, nil
			})
			_, _ = Exchange(context.Background(), doer, endpoint, testForm(), fixedNow, Policy{})
			if !closer.closed {
				t.Fatal("response body must be closed")
			}
		})
	}
}

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
