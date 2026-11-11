package githubwif

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (fn roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

const fixtureOIDCURL = "https://pipelines.actions.githubusercontent.com/oidc/token?api-version=7.0&job=fixture"

func exchangeBinding() (Contract, Mapping) {
	p := Contract{"https://example.invalid", "example/repo", "refs/heads/main", "example/repo/.github/workflows/review.yml@refs/heads/main", "staging"}
	return p, Mapping{ProviderID: "provider-fixture", ServiceAccountID: "service-fixture", RepositoryID: 123, RepositoryOwnerID: 456, WorkflowSHA: strings.Repeat("a", 40), Subject: "repo:example/repo:environment:staging"}
}

func exchangeClaims(p Contract, mapping Mapping, now time.Time) map[string]any {
	return map[string]any{
		"iss": Issuer, "aud": p.Audience, "repository": p.Repository, "ref": p.Ref, "workflow_ref": p.WorkflowRef,
		"environment": p.Environment, "event_name": probeEvent, "sub": mapping.Subject, "iat": now.Unix(), "nbf": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(), "repository_id": "123", "repository_owner_id": "456", "workflow_sha": mapping.WorkflowSHA,
	}
}

func fixtureJWT(claims map[string]any) string {
	payload, _ := json.Marshal(claims)
	return "fixture." + base64.RawURLEncoding.EncodeToString(payload) + ".fixture"
}

func TestRuntimeExchange(t *testing.T) {
	p, mapping := exchangeBinding()
	for _, mode := range []string{"direct", "sdk", "sdk-failure", "wrong-subject", "claim-drift", "expired", "overlong-lifetime", "inconsistent-lifetime", "oidc-redirect", "exchange-redirect", "exchange-rejected", "transport-failure"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			claims := exchangeClaims(p, mapping, now)
			switch mode {
			case "wrong-subject":
				claims["sub"] = "repo:example/repo:environment:production"
			case "claim-drift":
				claims["workflow_sha"] = strings.Repeat("b", 40)
			}
			subject := fixtureJWT(claims)
			result := TokenResponse{"fixture-access", "Bearer", accessTokenType, 120, now.Add(2 * time.Minute).Unix()}
			switch mode {
			case "expired":
				result.ExpiresAt = now.Unix()
			case "overlong-lifetime":
				result.ExpiresAt = now.Add(6 * time.Minute).Unix()
				result.ExpiresIn = 360
			case "inconsistent-lifetime":
				result.ExpiresIn = 1
			}
			var sdk SDKExchange
			var sdkCalls int
			if mode == "sdk" || mode == "sdk-failure" {
				sdk = func(_ context.Context, gotMapping Mapping, gotSubject string) (TokenResponse, error) {
					sdkCalls++
					if gotMapping != mapping || gotSubject != subject {
						t.Error("incorrect SDK binding")
					}
					if mode == "sdk-failure" {
						return TokenResponse{}, errors.New("sensitive failure marker")
					}
					return result, nil
				}
			}
			e, err := NewExchange(p, mapping, fixtureOIDCURL, "job-fixture", sdk)
			if err != nil {
				t.Fatal(err)
			}
			var oidc, exchange int
			e.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				var data []byte
				redirect := &http.Response{
					StatusCode: http.StatusFound,
					Header:     http.Header{"Location": []string{"https://redirect.invalid/"}},
					Body:       io.NopCloser(strings.NewReader("sensitive failure marker")),
				}
				switch r.URL.Host {
				case "pipelines.actions.githubusercontent.com":
					oidc++
					query := r.URL.Query()
					if r.Method != http.MethodGet || r.URL.Path != "/oidc/token" || r.Header.Get("Authorization") != "Bearer job-fixture" ||
						query.Get("audience") != p.Audience || len(query["audience"]) != 1 || query.Get("api-version") != "7.0" || query.Get("job") != "fixture" {
						t.Error("incorrect job token request")
					}
					if mode == "oidc-redirect" {
						return redirect, nil
					}
					data, _ = json.Marshal(map[string]string{"value": subject})
				case "auth.openai.com":
					exchange++
					var req map[string]string
					want := map[string]string{
						"grant_type":           "urn:ietf:params:oauth:grant-type:token-exchange",
						"subject_token_type":   "urn:ietf:params:oauth:token-type:jwt",
						"subject_token":        subject,
						"identity_provider_id": mapping.ProviderID,
						"service_account_id":   mapping.ServiceAccountID,
					}
					if r.Method != http.MethodPost || r.URL.String() != "https://auth.openai.com/oauth/token" || r.Header.Get("Authorization") != "" ||
						r.Header.Get("Content-Type") != "application/json" || r.GetBody != nil || json.NewDecoder(r.Body).Decode(&req) != nil || !maps.Equal(req, want) {
						t.Error("incorrect exchange request")
					}
					switch mode {
					case "exchange-redirect":
						return redirect, nil
					case "exchange-rejected":
						return &http.Response{StatusCode: http.StatusForbidden, Body: redirect.Body}, nil
					case "transport-failure":
						return nil, errors.New("sensitive failure marker")
					}
					data, _ = json.Marshal(result)
				default:
					t.Error("unexpected outbound host")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
			})
			if mode == "direct" || mode == "sdk" {
				var wg sync.WaitGroup
				for range 6 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						token, err := e.Get(context.Background())
						if err != nil || token.token != "fixture-access" || token.ExpiresAt().Unix() != result.ExpiresAt {
							t.Errorf("exchange failed: %v", err)
						}
					}()
				}
				wg.Wait()
			} else if _, err = e.Get(context.Background()); err == nil || strings.Contains(err.Error(), "sensitive failure marker") {
				t.Fatal("failed binding accepted or raw failure reflected")
			}
			wantExchange, wantSDK := 1, 0
			switch mode {
			case "wrong-subject", "claim-drift", "oidc-redirect":
				wantExchange = 0
			case "sdk", "sdk-failure":
				wantExchange, wantSDK = 0, 1
			}
			if oidc != 1 || exchange != wantExchange || sdkCalls != wantSDK {
				t.Fatalf("unexpected calls: OIDC %d, exchange %d, SDK %d", oidc, exchange, sdkCalls)
			}
		})
	}
}

func TestExchangeRejectsOutOfScopeClaims(t *testing.T) {
	p, mapping := exchangeBinding()
	now := time.Now()
	for claim, wrong := range map[string]any{
		"iss": "https://issuer.invalid", "aud": []string{p.Audience}, "repository": "another/repo",
		"repository_id": "124", "repository_owner_id": "457", "ref": "refs/heads/topic", "event_name": "pull_request",
		"workflow_ref": "example/repo/.github/workflows/other.yml@refs/heads/main", "environment": "production",
		"job_workflow_ref": "another/repo/.github/workflows/reusable.yml@refs/heads/main", "job_workflow_sha": strings.Repeat("b", 40),
		"sub": "repo:example/repo:environment:production", "exp": now.Unix(), "iat": now.Add(time.Minute).Unix(), "nbf": now.Add(time.Minute).Unix(),
	} {
		t.Run(claim, func(t *testing.T) {
			claims := exchangeClaims(p, mapping, now)
			claims[claim] = wrong
			if _, err := p.checkSubject(fixtureJWT(claims), mapping, now); err == nil {
				t.Fatal("accepted claim outside the admitted binding")
			}
		})
	}
}

func TestExchangeRejectsInvalidOIDCEndpoints(t *testing.T) {
	p, mapping := exchangeBinding()
	for _, endpoint := range []string{
		"http://pipelines.actions.githubusercontent.com/oidc",
		"https://pipelines.actions.githubusercontent.com:443/oidc",
		"https://pipelines.actions.githubusercontent.com:",
		"https://pipelines.actions.githubusercontent.com./oidc",
		"https://pipelines.actions.githubusercontent.com.example.invalid/oidc",
		"https://other.pipelines.actions.githubusercontent.com/oidc",
		"https://pipelines_.actions.githubusercontent.com/oidc",
		"https://user@pipelines.actions.githubusercontent.com/oidc",
		"https://token.actions.githubusercontent.com/.well-known/openid-configuration",
		"https://pipelines.actions.githubusercontent.com",
		"https://pipelines.actions.githubusercontent.com/oidc#",
		"https://pipelines.actions.githubusercontent.com/oidc#fragment",
		"https://pipelines.actions.githubusercontent.com/oidc?api-version=%ZZ",
		fixtureOIDCURL + "&audience=wrong",
		fixtureOIDCURL + "&audience=" + p.Audience + "&%61udience=" + p.Audience,
	} {
		if _, err := NewExchange(p, mapping, endpoint, "job-fixture", nil); err == nil {
			t.Errorf("accepted invalid OIDC endpoint %s", endpoint)
		}
	}
	mapping.Subject = ""
	if _, err := NewExchange(p, mapping, fixtureOIDCURL, "job-fixture", nil); err == nil {
		t.Error("accepted a mapping without a pinned subject")
	}
}

func TestExchangePreservesOIDCRequestURL(t *testing.T) {
	p, mapping := exchangeBinding()
	const endpoint = "https://run-actions-3-azure-eastus.actions.githubusercontent.com/fixture//idtoken/fixture?api-version=7.0&job=a%20b"
	const audience = "&audience=https%3A%2F%2Fexample.invalid"
	for _, requestURL := range []string{endpoint, endpoint + audience} {
		e, err := NewExchange(p, mapping, requestURL, "job-fixture", nil)
		if err != nil {
			t.Fatal(err)
		}
		if e.requestURL != endpoint+audience {
			t.Error("OIDC request did not preserve the injected path and query")
		}
	}
}
