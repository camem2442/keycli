package providerlifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (t roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return t(r) }
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func clientFixture(t *testing.T, fn roundTrip) Client {
	t.Helper()
	c, e := NewClient(func(r *http.Request) error { r.Header.Set("Authorization", "Bearer test-only"); return nil }, fn)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func authorized(context.Context, string, string, string) error { return nil }
func denied(context.Context, string, string, string) error {
	return errors.New("owner did not authorize")
}

func TestGitHubDeleteOnlyGenerationSecretAfterGrant(t *testing.T) {
	count := 0
	c := clientFixture(t, func(r *http.Request) (*http.Response, error) {
		count++
		if r.Method != "DELETE" || r.URL.String() != "https://api.github.com/repos/demo/repo/actions/secrets/KEY_GEN_1" || r.Header.Get("Authorization") == "" {
			t.Fatal("wrong GitHub effect")
		}
		return response(204, ""), nil
	})
	x, e := c.DeleteGitHubGenerationSecret(context.Background(), authorized, "demo/repo", "KEY_GEN_1", 123, 456, 1)
	if e != nil || x.Provider != "github" || x.Status != "ACKNOWLEDGED_UNVERIFIED" || x.AuthorizesRevocation || count != 1 {
		t.Fatalf("wrong GH result %+v %v", x, e)
	}
	if _, err := c.DeleteGitHubGenerationSecret(context.Background(), denied, "demo/repo", "KEY_GEN_1", 123, 456, 1); err == nil {
		t.Fatal("denied grant sent effect")
	}
	if _, err := c.DeleteGitHubGenerationSecret(context.Background(), authorized, "demo/repo", "KEY_GEN_2", 123, 456, 1); err == nil {
		t.Fatal("wrong generation sent effect")
	}
	if count != 1 {
		t.Fatal("extra GitHub mutation")
	}
}
func TestCloudflareTokenIssuerHasSingleScopedPolicyAndNoValueLeak(t *testing.T) {
	account := strings.Repeat("a", 32)
	perm := strings.Repeat("b", 32)
	token := strings.Repeat("c", 32)
	count := 0
	secretValue := "fixture-credential-never-print"
	c := clientFixture(t, func(r *http.Request) (*http.Response, error) {
		count++
		if r.Method != "POST" || r.URL.Path != "/client/v4/accounts/"+account+"/tokens" {
			t.Fatal("wrong CF issuer")
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"permission_groups":[{"id":"`+perm+`"}]`) || !strings.Contains(string(b), `"com.cloudflare.api.account.`+account+`":"*"`) || !strings.Contains(string(b), `"expires_on":`) || strings.Contains(string(b), secretValue) {
			t.Fatalf("unsafe token grant body %s", b)
		}
		return response(200, fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":"kc-rot-demo","value":%q}}`, token, secretValue)), nil
	})
	sinkCalls := 0
	ret, err := c.CreateCloudflareAccountToken(context.Background(), authorized, account, "kc-rot-demo", perm, time.Now().Add(2*time.Hour), func(_ context.Context, b []byte) error {
		sinkCalls++
		if string(b) != secretValue {
			t.Fatal("incorrect value passed to trusted sink")
		}
		return nil
	})
	if err != nil || ret.CredentialID != token || strings.Contains(fmt.Sprint(ret), secretValue) || sinkCalls != 1 || count != 1 {
		t.Fatalf("cf issue outcome: %+v %v", ret, err)
	}
	if _, err := c.CreateCloudflareAccountToken(context.Background(), denied, account, "kc-rot-demo", perm, time.Now().Add(2*time.Hour), func(context.Context, []byte) error { return nil }); err == nil || count != 1 {
		t.Fatal("denied issuer request executed")
	}
	if _, err := c.CreateCloudflareAccountToken(context.Background(), authorized, account, "kc-rot-demo", perm, time.Time{}, func(context.Context, []byte) error { return nil }); err == nil || count != 1 {
		t.Fatal("issuer accepted an indefinite token")
	}
}
func TestCloudflareTokenSinkFailureIsNotSafeToRetry(t *testing.T) {
	account := strings.Repeat("a", 32)
	id := strings.Repeat("b", 32)
	c := clientFixture(t, func(*http.Request) (*http.Response, error) {
		return response(200, fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":"kc-rot-demo","value":"once-only"}}`, id)), nil
	})
	_, err := c.CreateCloudflareAccountToken(context.Background(), authorized, account, "kc-rot-demo", id, time.Now().Add(2*time.Hour), func(context.Context, []byte) error { return errors.New("private store failed") })
	if err == nil || strings.Contains(err.Error(), "once-only") {
		t.Fatal("unsafe failure reporting")
	}
}
func TestCloudflareRevokerOnlyExactTokenID(t *testing.T) {
	account := strings.Repeat("a", 32)
	token := strings.Repeat("b", 32)
	calls := 0
	c := clientFixture(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "DELETE" || r.URL.Path != "/client/v4/accounts/"+account+"/tokens/"+token {
			t.Fatal("wrong CF revoke")
		}
		return response(200, `{"success":true,"result":{"id":"`+token+`"}}`), nil
	})
	got, err := c.DeleteCloudflareAccountToken(context.Background(), authorized, account, token)
	if err != nil || got.CredentialID != token {
		t.Fatalf("CF revoke %v %v", got, err)
	}
	if _, err := c.DeleteCloudflareAccountToken(context.Background(), denied, account, token); err == nil || calls != 1 {
		t.Fatal("used denied revoker")
	}
}
func TestCloudflareRevokerRechecksGrantAfterSigning(t *testing.T) {
	account, token := strings.Repeat("a", 32), strings.Repeat("b", 32)
	grantCurrent := true
	expired := errors.New("revocation grant expired")
	client, err := NewClient(func(req *http.Request) error {
		req.Header.Set("Authorization", "Bearer fixture")
		grantCurrent = false
		return nil
	}, roundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("expired revocation grant reached the provider")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	check := func(context.Context, string, string, string) error {
		if !grantCurrent {
			return expired
		}
		return nil
	}
	if _, err := client.DeleteCloudflareAccountToken(context.Background(), check, account, token); err == nil {
		t.Fatalf("expected expired grant, got %v", err)
	}
}

func TestOraclePendingAndScheduledVersionUseExactSignedIfMatch(t *testing.T) {
	id := "ocid1.vaultsecret.oc1.ap-osaka-1.synthetic"
	cnt := 0
	c := clientFixture(t, func(r *http.Request) (*http.Response, error) {
		cnt++
		if r.URL.Host != "vaults.ap-osaka-1.oci.oraclecloud.com" || r.Header.Get("If-Match") != "etag-pinned" || r.Header.Get("Authorization") == "" {
			t.Fatal("wrong OCI precondition")
		}
		b, _ := io.ReadAll(r.Body)
		switch r.Method {
		case "PUT":
			if r.URL.Path != "/20180608/secrets/"+id || !strings.Contains(string(b), `"stage":"PENDING"`) || !strings.Contains(string(b), `"contentType":"BASE64"`) {
				t.Fatal("not a pending version")
			}
			return response(200, fmt.Sprintf(`{"id":%q,"lifecycleState":"ACTIVE"}`, id)), nil
		case "POST":
			if r.URL.Path != "/20180608/secrets/"+id+"/version/7/actions/scheduleDeletion" || !strings.Contains(string(b), `"timeOfDeletion"`) {
				t.Fatal("unexpected OCI revoke")
			}
			return response(204, ""), nil
		}
		t.Fatal("unexpected OCI effect")
		return nil, errors.New("unexpected")
	})
	got, err := c.StageOraclePendingVaultVersion(context.Background(), authorized, "ap-osaka-1", id, "etag-pinned", []byte("not-a-real-key"))
	if err != nil || got.Status != "ACKNOWLEDGED_UNVERIFIED" {
		t.Fatalf("pending=%v %v", got, err)
	}
	got, err = c.ScheduleOracleOldVersionDeletion(context.Background(), authorized, "ap-osaka-1", id, "etag-pinned", 7, time.Now().Add(48*time.Hour))
	if err != nil || got.CredentialID != "7" {
		t.Fatalf("scheduled=%v %v", got, err)
	}
	if _, err := c.ScheduleOracleOldVersionDeletion(context.Background(), authorized, "ap-osaka-1", id, "etag-pinned", 7, time.Now().Add(time.Hour)); err == nil || cnt != 2 {
		t.Fatal("old version scheduled before safe delay")
	}
}
func TestTransportFailureAndRedirectAreUnknown(t *testing.T) {
	account := strings.Repeat("a", 32)
	token := strings.Repeat("b", 32)
	for _, scenario := range []string{"timeout", "redirect", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			c := clientFixture(t, func(r *http.Request) (*http.Response, error) {
				switch scenario {
				case "timeout":
					return nil, errors.New("network secret key never log")
				case "redirect":
					return response(302, `{"location":"evil.test"}`), nil
				default:
					return response(403, "access denied secret=never log"), nil
				}
			})
			_, err := c.DeleteCloudflareAccountToken(context.Background(), authorized, account, token)
			if err == nil || strings.Contains(err.Error(), "secret=") || strings.Contains(err.Error(), "never log") {
				t.Fatalf("wrong unknown-effect behavior: %v", err)
			}
		})
	}
}
