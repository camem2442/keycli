package ociwif

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestInstanceSubjectToken(t *testing.T) {
	const endpoint = "https://idcs-fixture.identity.oraclecloud.com/oauth2/v1/token"
	payload := `{"iss":"https://identity.oraclecloud.com/","aud":["https://idcs-fixture.identity.oraclecloud.com"],"sub_type":"instance","ipst_instance":"fixture-instance","ipst_compartment":"fixture-compartment","domain_id":"fixture-domain","ca_ocid":"fixture-tenancy","iat":1782365834,"exp":1782369434}`
	jwt := "fixture." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".fixture"
	for _, tc := range []struct {
		name        string
		status      int
		wantSuccess bool
	}{
		{"success", http.StatusOK, true},
		{"redirect", http.StatusTemporaryRedirect, false},
		{"provider failure", http.StatusServiceUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewInstanceSigner("us-phoenix-1", endpoint)
			if err != nil {
				t.Fatal(err)
			}
			s.sign = func(req *http.Request) error {
				req.Header.Set("Authorization", "synthetic-signature")
				return nil
			}
			calls := 0
			s.client.Transport = instanceTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.URL.String() != endpoint || req.Method != http.MethodPost ||
					req.Header.Get("Authorization") != "synthetic-signature" || req.GetBody != nil {
					t.Fatal("unexpected OCI request")
				}
				if err := req.ParseForm(); err != nil || len(req.PostForm) != 3 ||
					req.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:token-exchange" ||
					req.PostForm.Get("scope") != "urn:opc:idm:__myscopes__" ||
					req.PostForm.Get("requested_token_type") != accessTokenType {
					t.Fatal("invalid instance token request form")
				}
				return &http.Response{
					StatusCode: tc.status, Header: http.Header{"Location": {"https://other.example.invalid/"}},
					Body: io.NopCloser(strings.NewReader(`{"access_token":"` + jwt + `"}`)),
				}, nil
			})
			result, err := s.SubjectToken(context.Background())
			if calls != 1 || (err == nil) != tc.wantSuccess {
				t.Fatalf("unexpected OCI exchange result: %v", err)
			}
			if tc.wantSuccess && (result.Raw != jwt || result.Claims.InstanceOCID != "fixture-instance" ||
				len(result.Claims.Audience) != 1 || result.Claims.Audience[0] != "https://idcs-fixture.identity.oraclecloud.com") {
				t.Fatal("subject token or claims changed")
			}
		})
	}
	if _, err := decodeClaims("fixture." + base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(payload, `"aud":`, `"aud":"duplicate","aud":`, 1))) + ".fixture"); err == nil {
		t.Fatal("accepted duplicate JWT claim")
	}
}

type instanceTransport func(*http.Request) (*http.Response, error)

func (fn instanceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestInstanceSDKDestinations(t *testing.T) {
	s, err := NewInstanceSigner("us-phoenix-1", "https://idcs-fixture.identity.oraclecloud.com/oauth2/v1/token")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, target string
		want           bool
	}{
		{http.MethodGet, metadataRegion, true},
		{http.MethodGet, metadataRegion + "?redirect=1", false},
		{http.MethodGet, "http://169.254.169.254/opc/v1/identity/key.pem", false},
		{http.MethodPost, "https://auth.us-phoenix-1.oraclecloud.com/v1/x509", true},
		{http.MethodPost, "https://auth.us-ashburn-1.oraclecloud.com/v1/x509", false},
	} {
		req, err := http.NewRequest(tc.method, tc.target, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer Oracle")
		if s.allowsSDKRequest(req) != tc.want {
			t.Fatalf("unexpected admission for %s %s", tc.method, tc.target)
		}
	}
}
