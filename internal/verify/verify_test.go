package verify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/verify"
)

func TestCloudflareAgentVerifyOK(t *testing.T) {
	srv := newCFServer(t, map[string]int{
		"/client/v4/user/tokens/verify": 200,
		"/client/v4/accounts":           200,
	})
	t.Setenv("CF_API_BASE", srv.URL+"/client/v4")

	out, err := verify.Run(context.Background(), "cloudflare_agent", "", []byte("test-token"), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Block || out.VerifyStatus != 200 {
		t.Fatalf("out %+v", out)
	}
	if len(out.ProbeLines) != 1 || !strings.Contains(out.ProbeLines[0], `"count":2`) {
		t.Fatalf("probes %v", out.ProbeLines)
	}
}

func TestCloudflareAgentVerifyFail(t *testing.T) {
	srv := newCFServer(t, map[string]int{
		"/client/v4/user/tokens/verify": 401,
	})
	t.Setenv("CF_API_BASE", srv.URL+"/client/v4")

	out, err := verify.Run(context.Background(), "cloudflare_agent", "", []byte("bad"), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || !out.Block {
		t.Fatalf("expected block %+v", out)
	}
}

func TestCloudflareBillingNarrowPass(t *testing.T) {
	srv := newCFServer(t, map[string]int{
		"/client/v4/user/tokens/verify":                                                  200,
		"/client/v4/accounts/b398955bcfe917a4af61d179af967b6a/billing/subscriptions":     200,
		"/client/v4/accounts/b398955bcfe917a4af61d179af967b6a/billing/v1/billable-usage": 200,
		"/client/v4/user/billing/profile":                                                403,
		"/client/v4/accounts/b398955bcfe917a4af61d179af967b6a/billing/spend-policies":    404,
	})
	t.Setenv("CF_API_BASE", srv.URL+"/client/v4")

	out, err := verify.Run(context.Background(), "cloudflare_billing_read", "", []byte("tok"), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Block {
		t.Fatalf("narrow pass expected %+v", out)
	}
	if !out.Warn {
		t.Fatalf("expected warn on profile auth_fail")
	}
}

func TestGitHubAgentVerifyOK(t *testing.T) {
	repo := "camem2442/mock-exam-timer"
	branch := "main"
	srv := newGHServer(t, ghRoutes{
		"/user":                                        userJSON(`{"login":"camem2442"}`),
		"/repos/" + repo + "/branches/main":            branchJSON(true),
		"/repos/" + repo + "/rulesets":                 `[]`,
		"/repos/" + repo + "/branches/main/protection": protectionJSON([]string{"ci/lint", "ci/test"}),
	})
	t.Setenv("GH_API_BASE", srv.URL)
	t.Setenv("GH_PROBE_REPO", repo)
	t.Setenv("GH_PROBE_BRANCH", branch)

	out, err := verify.Run(context.Background(), "github_agent", "", []byte("gh-test-token"), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Block || out.VerifyStatus != 200 {
		t.Fatalf("out %+v", out)
	}
	if !strings.Contains(strings.Join(out.ProbeLines, "\n"), `"login":"camem2442"`) {
		t.Fatalf("probes %v", out.ProbeLines)
	}
	if !strings.Contains(strings.Join(out.ProbeLines, "\n"), `"ci/lint"`) {
		t.Fatalf("protection contexts missing %v", out.ProbeLines)
	}
}

func TestGitHubAgentUserFail(t *testing.T) {
	srv := newGHServer(t, ghRoutes{
		"/user": http.StatusUnauthorized,
	})
	t.Setenv("GH_API_BASE", srv.URL)

	out, err := verify.Run(context.Background(), "github_agent", "", []byte("bad"), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || !out.Block {
		t.Fatalf("expected block %+v", out)
	}
}

func TestGitHubAgentRulesets403Blocks(t *testing.T) {
	repo := "camem2442/mock-exam-timer"
	srv := newGHServer(t, ghRoutes{
		"/user":                             userJSON(`{"login":"camem2442"}`),
		"/repos/" + repo + "/branches/main": branchJSON(false),
		"/repos/" + repo + "/rulesets":      http.StatusForbidden,
	})
	t.Setenv("GH_PROBE_BRANCH", "main")
	t.Setenv("GH_API_BASE", srv.URL)
	t.Setenv("GH_PROBE_REPO", repo)

	out, err := verify.Run(context.Background(), "github_agent", "", []byte("tok"), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || !out.Block {
		t.Fatalf("rulesets 403 should block %+v", out)
	}
}

func TestGitHubAgentUnprotectedSkipsProtection(t *testing.T) {
	repo := "org/repo"
	srv := newGHServer(t, ghRoutes{
		"/user":                             userJSON(`{"login":"u"}`),
		"/repos/" + repo + "/branches/main": branchJSON(false),
		"/repos/" + repo + "/rulesets":      `[]`,
	})
	t.Setenv("GH_API_BASE", srv.URL)
	t.Setenv("GH_PROBE_REPO", repo)
	t.Setenv("GH_PROBE_BRANCH", "main")

	out, err := verify.Run(context.Background(), "github_agent", "", []byte("tok"), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Block {
		t.Fatalf("expected pass %+v", out)
	}
	for _, line := range out.ProbeLines {
		if strings.Contains(line, `"probe":"protection"`) {
			t.Fatalf("protection should be skipped %v", out.ProbeLines)
		}
	}
}

func TestCloudflareBillingNarrowFail(t *testing.T) {
	srv := newCFServer(t, map[string]int{
		"/client/v4/user/tokens/verify":                                                  200,
		"/client/v4/accounts/b398955bcfe917a4af61d179af967b6a/billing/subscriptions":     403,
		"/client/v4/accounts/b398955bcfe917a4af61d179af967b6a/billing/v1/billable-usage": 200,
	})
	t.Setenv("CF_API_BASE", srv.URL+"/client/v4")

	out, err := verify.Run(context.Background(), "cloudflare_billing_read", "", []byte("tok"), srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || !out.Block {
		t.Fatalf("expected block %+v", out)
	}
}

type ghRoutes map[string]interface{}

func newGHServer(t *testing.T, routes ghRoutes) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := r.URL.EscapedPath()
		if path == "" {
			path = r.URL.Path
		}
		v, ok := routes[path]
		if !ok {
			v, ok = routes[r.URL.Path]
		}
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch x := v.(type) {
		case int:
			w.WriteHeader(x)
		case string:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(x))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
}

func userJSON(inner string) string { return inner }
func branchJSON(protected bool) string {
	if protected {
		return `{"name":"b","protected":true}`
	}
	return `{"name":"b","protected":false}`
}
func protectionJSON(contexts []string) string {
	b, _ := json.Marshal(map[string]interface{}{
		"required_status_checks": map[string]interface{}{
			"contexts": contexts,
		},
	})
	return string(b)
}

func newCFServer(t *testing.T, statusByPath map[string]int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		st, ok := statusByPath[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1003}]}`))
			return
		}
		w.WriteHeader(st)
		if st == http.StatusOK {
			switch {
			case strings.HasSuffix(r.URL.Path, "/tokens/verify"):
				_, _ = w.Write([]byte(`{"success":true,"errors":[]}`))
			case strings.HasSuffix(r.URL.Path, "/accounts"):
				_, _ = w.Write([]byte(`{"success":true,"result":[{},{}]}`))
			default:
				_, _ = w.Write([]byte(`{"success":true,"errors":[]}`))
			}
		} else if st == http.StatusUnauthorized || st == http.StatusForbidden {
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000}]}`))
		} else if st == http.StatusNotFound {
			_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":1003}]}`))
		} else {
			_, _ = w.Write([]byte(`{"success":false,"errors":[]}`))
		}
	}))
}

func TestIntegrationRealCF(t *testing.T) {
	if os.Getenv("KC_SET_VERIFY_INTEGRATION") == "" {
		t.Skip("set KC_SET_VERIFY_INTEGRATION=1 to run")
	}
	if os.Getenv("CF_API_TOKEN") == "" {
		t.Skip("CF_API_TOKEN required")
	}
	out, err := verify.Run(context.Background(), "cloudflare_agent", "", []byte(os.Getenv("CF_API_TOKEN")), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("expected outcome")
	}
	b, _ := json.Marshal(out)
	t.Logf("outcome %s", b)
}
