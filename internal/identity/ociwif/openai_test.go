package ociwif

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

type exchangeRoundTripper func(*http.Request) (*http.Response, error)

func (f exchangeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDirectOpenAIExchange(t *testing.T) {
	input := ExchangeRequest{
		SourceToken: "synthetic.jwt.subject", IdentityProvider: "wip_synthetic",
		ServiceAccount: "svc_synthetic", Audience: testAudience,
	}
	const success = `{"access_token":"synthetic.openai.access","token_type":"Bearer","issued_token_type":"urn:ietf:params:oauth:token-type:access_token","expires_in":120,"expires_at":1794312120,"scope":"api.model.request"}`
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		failure error
		ok      bool
	}{
		{"success", http.StatusOK, success, nil, true},
		{"redirect with body", http.StatusTemporaryRedirect, input.SourceToken, nil, false},
		{"redirect without body", http.StatusSeeOther, input.SourceToken, nil, false},
		{"denied", http.StatusUnauthorized, input.SourceToken, nil, false},
		{"bad token", http.StatusOK, strings.Replace(success, "synthetic.openai.access", "unsafe,token", 1), nil, false},
		{"wrong token type", http.StatusOK, strings.Replace(success, "Bearer", "DPoP", 1), nil, false},
		{"wrong issued type", http.StatusOK, strings.Replace(success, accessTokenType, jwtTokenType, 1), nil, false},
		{"trailing JSON", http.StatusOK, success + "{}", nil, false},
		{"oversized", http.StatusOK, success + strings.Repeat(" ", maxExchangeBody), nil, false},
		{"transport error", 0, "", errors.New(input.SourceToken), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := NewOpenAIExchanger().(*openAIExchanger)
			tr := e.client.Transport.(*http.Transport)
			if tr.Proxy != nil || !tr.DisableKeepAlives || tr.TLSClientConfig.InsecureSkipVerify ||
				tr.TLSNextProto == nil || len(tr.TLSNextProto) != 0 || e.client.Timeout != exchangeTimeout {
				t.Fatal("transport is not configured for a direct, bounded and single attempt")
			}
			calls := 0
			e.client.Transport = exchangeRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPost || r.URL.String() != OpenAIExchangeEndpoint ||
					r.Header.Get("Content-Type") != "application/json" || r.GetBody != nil {
					t.Fatal("unexpected exchange endpoint, method, content type or replayable body")
				}
				defer r.Body.Close()
				var got map[string]string
				want := map[string]string{
					"grant_type":           tokenExchangeGrant,
					"subject_token_type":   jwtTokenType,
					"subject_token":        input.SourceToken,
					"identity_provider_id": input.IdentityProvider,
					"service_account_id":   input.ServiceAccount,
				}
				if json.NewDecoder(r.Body).Decode(&got) != nil || !reflect.DeepEqual(got, want) {
					t.Fatal("exchange request does not match documented JWT fields")
				}
				if tt.failure != nil {
					return nil, tt.failure
				}
				return &http.Response{StatusCode: tt.status, Header: http.Header{"Location": {"https://redirect.invalid/"}},
					Body: io.NopCloser(strings.NewReader(tt.body))}, nil
			})

			got, err := e.Exchange(context.Background(), input)
			if calls != 1 {
				t.Fatalf("expected exactly one attempt, got %d", calls)
			}
			if !tt.ok {
				if !errors.Is(err, ErrExchangeUnknown) || strings.Contains(err.Error(), input.SourceToken) || got != (TokenResponse{}) {
					t.Fatal("failure returned data or did not return a redacted unknown result")
				}
				return
			}
			if err != nil || got.AccessToken != "synthetic.openai.access" || got.TokenType != "Bearer" ||
				got.IssuedTokenType != accessTokenType || got.ExpiresIn != 120 || got.ExpiresAt != 1794312120 {
				t.Fatal("response did not preserve token and expiry fields")
			}
		})
	}
}
