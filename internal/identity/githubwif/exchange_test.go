package githubwif

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (fn roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestRuntimeExchange(t *testing.T) {
	p := Contract{"https://example.invalid", "example/repo", "refs/heads/main", "example/repo/.github/workflows/review.yml@refs/heads/main", "staging"}
	mapping := Mapping{ProviderID: "provider-fixture", ServiceAccountID: "service-fixture", RepositoryID: 123, RepositoryOwnerID: 456, WorkflowSHA: strings.Repeat("a", 40)}
	for _, mode := range []string{"direct", "sdk-failure", "claim-drift", "overlong-lifetime"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now()
			sha := mapping.WorkflowSHA
			if mode == "claim-drift" {
				sha = strings.Repeat("b", 40)
			}
			payload, _ := json.Marshal(map[string]any{
				"iss": Issuer, "aud": p.Audience, "repository": p.Repository, "ref": p.Ref, "workflow_ref": p.WorkflowRef,
				"environment": p.Environment, "event_name": probeEvent, "sub": "fixture", "iat": now.Unix(),
				"exp": now.Add(5 * time.Minute).Unix(), "repository_id": "123", "repository_owner_id": "456", "workflow_sha": sha,
			})
			subject := "fixture." + base64.RawURLEncoding.EncodeToString(payload) + ".fixture"
			var sdk SDKExchange
			if mode == "sdk-failure" {
				sdk = func(context.Context, Mapping, string) (TokenResponse, error) {
					return TokenResponse{}, errors.New("sensitive failure marker")
				}
			}
			e, err := NewExchange(p, mapping, "https://pipelines.actions.githubusercontent.com/oidc", "job-fixture", sdk)
			if err != nil {
				t.Fatal(err)
			}
			var oidc, exchange int
			e.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				var data []byte
				switch r.URL.Host {
				case "pipelines.actions.githubusercontent.com":
					oidc++
					if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer job-fixture" || r.URL.Query().Get("audience") != p.Audience {
						t.Error("incorrect job token request")
					}
					data, _ = json.Marshal(map[string]string{"value": subject})
				case "auth.openai.com":
					exchange++
					var req map[string]string
					if r.Method != "POST" || r.URL.String() != exchangeURL || r.Header.Get("Authorization") != "" ||
						json.NewDecoder(r.Body).Decode(&req) != nil || req["subject_token"] != subject ||
						req["identity_provider_id"] != mapping.ProviderID || req["service_account_id"] != mapping.ServiceAccountID {
						t.Error("incorrect exchange request")
					}
					exp := now.Add(2 * time.Minute).Unix()
					if mode == "overlong-lifetime" {
						exp = now.Add(6 * time.Minute).Unix()
					}
					data, _ = json.Marshal(TokenResponse{"fixture-access", "Bearer", accessTokenType, 3600, exp})
				default:
					t.Error("unexpected outbound host")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
			})
			if mode == "direct" {
				var wg sync.WaitGroup
				for range 6 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						token, err := e.Get(context.Background())
						if err != nil || token.token != "fixture-access" {
							t.Errorf("exchange failed: %v", err)
						}
					}()
				}
				wg.Wait()
			} else if _, err = e.Get(context.Background()); err == nil || strings.Contains(err.Error(), "sensitive failure marker") {
				t.Fatal("failed binding accepted or raw failure reflected")
			}
			want := 1
			if mode == "sdk-failure" || mode == "claim-drift" {
				want = 0
			}
			if oidc != 1 || exchange != want {
				t.Fatalf("unexpected calls: OIDC %d, exchange %d", oidc, exchange)
			}
		})
	}
}
