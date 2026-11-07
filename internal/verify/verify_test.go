package verify_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/verify"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func reply(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func mockClient(routes map[string]any) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") == "" {
			return reply(r, http.StatusUnauthorized, ""), nil
		}
		value, ok := routes[r.URL.EscapedPath()]
		if !ok {
			value, ok = routes[r.URL.Path]
		}
		if !ok {
			return reply(r, http.StatusNotFound, `{"success":false,"errors":[{"code":1003}]}`), nil
		}
		switch x := value.(type) {
		case int:
			return reply(r, x, `{"success":false,"errors":[{"code":10000}]}`), nil
		case string:
			return reply(r, http.StatusOK, x), nil
		default:
			return nil, errors.New("invalid fixture")
		}
	})}
}
func cfRoutes(verifyStatus int) map[string]any {
	v := any(`{"success":true,"errors":[]}`)
	if verifyStatus != http.StatusOK {
		v = verifyStatus
	}
	return map[string]any{"/client/v4/user/tokens/verify": v, "/client/v4/accounts": `{"success":true,"result":[{},{}]}`}
}
func githubRoutes(protected bool, protection any) map[string]any {
	branch := `{"protected":false}`
	if protected {
		branch = `{"protected":true}`
	}
	return map[string]any{"/user": `{"login":"fixture"}`, "/repos/org/repo/branches/main": branch, "/repos/org/repo/rulesets": `[]`, "/repos/org/repo/branches/main/protection": protection}
}
func githubEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GH_PROBE_REPO", "org/repo")
	t.Setenv("GH_PROBE_BRANCH", "main")
}
func TestCloudflareAgentVerifyOK(t *testing.T) {
	out, err := verify.Run(context.Background(), "cloudflare_agent", "", []byte("fixture-token"), mockClient(cfRoutes(200)))
	if err != nil || out == nil || out.Block || out.VerifyStatus != 200 {
		t.Fatalf("unexpected result: %+v %v", out, err)
	}
	if len(out.ProbeLines) != 1 || !strings.Contains(out.ProbeLines[0], `"count":2`) {
		t.Fatalf("unexpected probe: %v", out.ProbeLines)
	}
}
func TestCloudflareAgentVerifyFail(t *testing.T) {
	out, err := verify.Run(context.Background(), "cloudflare_agent", "", []byte("fixture"), mockClient(cfRoutes(401)))
	if err != nil || out == nil || !out.Block {
		t.Fatalf("expected block: %+v %v", out, err)
	}
}
func TestCloudflareBillingNarrow(t *testing.T) {
	for _, status := range []int{200, 403} {
		routes := cfRoutes(200)
		base := "/client/v4/accounts/b398955bcfe917a4af61d179af967b6a/billing/"
		if status == 200 {
			routes[base+"subscriptions"] = `{"success":true}`
		} else {
			routes[base+"subscriptions"] = status
		}
		routes[base+"v1/billable-usage"] = `{"success":true}`
		routes["/client/v4/user/billing/profile"] = 403
		routes[base+"spend-policies"] = 404
		out, err := verify.Run(context.Background(), "cloudflare_billing_read", "", []byte("fixture"), mockClient(routes))
		if err != nil || out == nil || out.Block != (status != 200) {
			t.Fatalf("status %d: %+v %v", status, out, err)
		}
		if status == 200 && !out.Warn {
			t.Fatal("profile warning missing")
		}
	}
}
func TestGitHubProtectedSuccess(t *testing.T) {
	githubEnv(t)
	routes := githubRoutes(true, `{"required_status_checks":{"contexts":["ci/lint"]}}`)
	out, err := verify.Run(context.Background(), "github_agent", "", []byte("fixture"), mockClient(routes))
	if err != nil || out == nil || out.Block || out.Warn || !strings.Contains(strings.Join(out.ProbeLines, "\n"), `"ci/lint"`) {
		t.Fatalf("unexpected: %+v %v", out, err)
	}
}
func TestGitHubProtectionDeniedBlocks(t *testing.T) {
	githubEnv(t)
	for _, status := range []int{401, 403, 500} {
		out, err := verify.Run(context.Background(), "github_agent", "", []byte("fixture"), mockClient(githubRoutes(true, status)))
		if err != nil || out == nil || !out.Block {
			t.Fatalf("status %d should block: %+v %v", status, out, err)
		}
	}
}
func TestGitHubUnprotectedOrMissingClassicRuleIsNotVerified(t *testing.T) {
	githubEnv(t)
	for _, scenario := range []struct {
		protected bool
		status    int
		label     string
	}{{false, 200, "not_verified"}, {true, 404, "not_found"}} {
		out, err := verify.Run(context.Background(), "github_agent", "", []byte("fixture"), mockClient(githubRoutes(scenario.protected, scenario.status)))
		if err != nil || out == nil || out.Block || !out.Warn || !strings.Contains(strings.Join(out.ProbeLines, "\n"), scenario.label) {
			t.Fatalf("expected warning: %+v %v", out, err)
		}
	}
}
func TestMalformedBranchOrRulesetBlocks(t *testing.T) {
	githubEnv(t)
	for _, path := range []string{"/repos/org/repo/branches/main", "/repos/org/repo/rulesets"} {
		routes := githubRoutes(true, `{}`)
		routes[path] = `{"unexpected":true}`
		out, err := verify.Run(context.Background(), "github_agent", "", []byte("fixture"), mockClient(routes))
		if err != nil || out == nil || !out.Block {
			t.Fatalf("%s should block: %+v %v", path, out, err)
		}
	}
}
func TestUnsafeEnvironmentOriginRejectedBeforeRequest(t *testing.T) {
	for _, tc := range []struct{ name, env, host string }{{"cloudflare_agent", "CF_API_BASE", "api.cloudflare.com"}, {"github_agent", "GH_API_BASE", "api.github.com"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, "http://attacker.invalid")
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.URL.Host != tc.host {
					t.Errorf("wrong origin %s", r.URL.Redacted())
				}
				return reply(r, 401, `{}`), nil
			})}
			out, err := verify.Run(context.Background(), tc.name, "", []byte("fixture"), client)
			if err == nil || out != nil || calls != 0 || strings.Contains(err.Error(), "attacker") {
				t.Fatalf("unsafe origin must fail before request: %+v %v calls %d", out, err, calls)
			}
		})
	}
}
func TestRedirectNeverForwardsBearer(t *testing.T) {
	for _, name := range []string{"cloudflare_agent", "github_agent"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				resp := reply(r, 302, "")
				resp.Header.Set("Location", "http://attacker.invalid/steal")
				return resp, nil
			})}
			out, err := verify.Run(context.Background(), name, "", []byte("fixture"), client)
			if err != nil || out == nil || !out.Block || calls != 1 {
				t.Fatalf("redirect followed: %+v %v calls %d", out, err, calls)
			}
		})
	}
}
func TestGitHubRulesets403Blocks(t *testing.T) {
	githubEnv(t)
	routes := githubRoutes(false, 200)
	routes["/repos/org/repo/rulesets"] = 403
	out, err := verify.Run(context.Background(), "github_agent", "", []byte("fixture"), mockClient(routes))
	if err != nil || out == nil || !out.Block {
		t.Fatalf("expected block: %+v %v", out, err)
	}
}
func TestIntegrationRealCF(t *testing.T) {
	if os.Getenv("KC_SET_VERIFY_INTEGRATION") == "" || os.Getenv("CF_API_TOKEN") == "" {
		t.Skip("explicit integration credentials required")
	}
	out, err := verify.Run(context.Background(), "cloudflare_agent", "", []byte(os.Getenv("CF_API_TOKEN")), nil)
	if err != nil || out == nil {
		t.Fatalf("integration failed: %v", err)
	}
	b, _ := json.Marshal(out)
	t.Logf("outcome %s", b)
}

func TestGitHubProtectionParsesCheckObjectsAndRejectsMalformed(t *testing.T) {
	githubEnv(t)
	valid := `{"required_status_checks":{"contexts":["ci/old"],"checks":[{"context":"ci/new"},{"context":"ci/old"}]}}`
	out, err := verify.Run(context.Background(), "github_agent", "", []byte("fixture"), mockClient(githubRoutes(true, valid)))
	if err != nil || out == nil || out.Block || !strings.Contains(strings.Join(out.ProbeLines, "\n"), `"ci/new"`) {
		t.Fatalf("check objects missing: %+v %v", out, err)
	}
	for _, invalid := range []string{`{}`, `{"required_status_checks":42}`, `{"required_status_checks":{"checks":[{}]}}`} {
		out, err := verify.Run(context.Background(), "github_agent", "", []byte("fixture"), mockClient(githubRoutes(true, invalid)))
		if err != nil || out == nil || !out.Block {
			t.Fatalf("malformed protection accepted: %+v %v", out, err)
		}
	}
}
func TestGitHubProtectionTransportFailureBlocks(t *testing.T) {
	githubEnv(t)
	base := mockClient(githubRoutes(true, `{}`)).Transport
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/protection") {
			return nil, errors.New("fixture transport failure")
		}
		return base.RoundTrip(r)
	})}
	out, err := verify.Run(context.Background(), "github_agent", "", []byte("fixture"), client)
	if err != nil || out == nil || !out.Block {
		t.Fatalf("transport failure should block: %+v %v", out, err)
	}
}

func TestCanonicalEnvironmentOriginAllowed(t *testing.T) {
	for _, tc := range []struct{ name, env, origin string }{{"cloudflare_agent", "CF_API_BASE", "https://api.cloudflare.com/client/v4"}, {"github_agent", "GH_API_BASE", "https://api.github.com"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, tc.origin)
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return reply(r, 401, `{}`), nil })}
			out, err := verify.Run(context.Background(), tc.name, "", []byte("fixture"), client)
			if err != nil || out == nil || !out.Block || calls != 1 {
				t.Fatalf("canonical origin should execute once: %+v %v calls %d", out, err, calls)
			}
		})
	}
}

func TestMalformedOverrideNeverFallsThroughToProvider(t *testing.T) {
	for _, tc := range []struct{ name, env string }{{"cloudflare_agent", "CF_API_BASE"}, {"cloudflare_billing_read", "CF_API_BASE"}, {"github_agent", "GH_API_BASE"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.env, "////")
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { calls++; return reply(r, 401, `{}`), nil })}
			out, err := verify.Run(context.Background(), tc.name, "", []byte("fixture"), client)
			if err == nil || out != nil || calls != 0 {
				t.Fatalf("malformed origin must not call provider: %+v %v calls %d", out, err, calls)
			}
		})
	}
}

// With default byte preservation, CRLF within a single-line bearer token must
// fail verification before any provider request (opt-in --normalize is CLI-owned).
func TestCloudflareAgentVerifyRejectsCRLFInTokenWithoutNormalize(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return reply(r, http.StatusOK, `{"success":true,"result":[]}`), nil
	})}
	out, err := verify.Run(context.Background(), "cloudflare_agent", "", append([]byte("fixture-token"), byte(13), byte(10)), client)
	if err == nil || out != nil || calls != 0 {
		t.Fatalf("CRLF token must be rejected before network, got outcome=%+v err=%v calls=%d", out, err, calls)
	}
}
