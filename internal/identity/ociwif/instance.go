package ociwif

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/common/auth"
)

const metadataRegion = "http://169.254.169.254/opc/v2/instance/region"

var (
	_, sdkDebugAtStartup = os.LookupEnv("OCI_GO_SDK_DEBUG")
	regionName           = regexp.MustCompile(`^[a-z]{2}-[a-z0-9]+(?:-[a-z0-9]+)*-[1-9][0-9]*$`)
	domainHost           = regexp.MustCompile(`^idcs-[a-z0-9]+(?:\.[a-z0-9-]+)?\.identity\.oraclecloud\.com$`)
	// The SDK initializes its metadata paths in package-level variables.
	instanceInit = make(chan struct{}, 1)
)

type InstanceSigner struct {
	region, endpoint, federation string
	client                       *http.Client
	sign                         func(*http.Request) error
}

func NewInstanceSigner(region, endpoint string) (*InstanceSigner, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || !domainHost.MatchString(u.Host) ||
		endpoint != "https://"+u.Host+"/oauth2/v1/token" || !regionName.MatchString(region) {
		return nil, errors.New("invalid OCI instance signer endpoint or region")
	}
	return &InstanceSigner{
		region: region, endpoint: endpoint, federation: "https://auth." + region + ".oraclecloud.com/v1/x509",
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
				TLSNextProto:    map[string]func(string, *tls.Conn) http.RoundTripper{},
			},
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

type sdkDispatcher func(*http.Request) (*http.Response, error)

func (d sdkDispatcher) Do(r *http.Request) (*http.Response, error) { return d(r) }

func (s *InstanceSigner) sdkSigner(ctx context.Context) (common.HTTPRequestSigner, error) {
	if _, debug := os.LookupEnv("OCI_GO_SDK_DEBUG"); sdkDebugAtStartup || debug {
		return nil, ErrSourceUnavailable
	}
	if os.Getenv("OCI_METADATA_BASE_URL") != "" || os.Getenv("OCI_SDK_AUTH_CLIENT_REGION_URL") != "" {
		return nil, ErrSourceUnavailable
	}
	// Resolve the region once with the caller's deadline. The SDK retries
	// region lookup using sleeps that do not observe request cancellation.
	regionRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataRegion, nil)
	if err != nil {
		return nil, ErrSourceUnavailable
	}
	regionRequest.Header.Set("Authorization", "Bearer Oracle")
	response, err := s.client.Do(regionRequest)
	if err != nil {
		return nil, ErrSourceUnavailable
	}
	region, readErr := io.ReadAll(io.LimitReader(response.Body, 129))
	response.Body.Close()
	if response.StatusCode != http.StatusOK || readErr != nil || strings.TrimSpace(string(region)) != s.region {
		return nil, ErrSourceUnavailable
	}
	select {
	case instanceInit <- struct{}{}:
	case <-ctx.Done():
		return nil, ErrSourceUnavailable
	}
	defer func() { <-instanceInit }()
	provider, err := auth.InstancePrincipalConfigurationProviderWithCustomClient(func(common.HTTPRequestDispatcher) (common.HTTPRequestDispatcher, error) {
		return sdkDispatcher(func(r *http.Request) (*http.Response, error) {
			if !s.allowsSDKRequest(r) {
				return nil, ErrSourceUnavailable
			}
			if r.URL.String() == metadataRegion {
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(s.region))}, nil
			}
			return s.client.Do(r.WithContext(ctx))
		}), nil
	})
	if err != nil {
		return nil, ErrSourceUnavailable
	}
	return common.DefaultRequestSigner(provider), nil
}

func (s *InstanceSigner) allowsSDKRequest(r *http.Request) bool {
	if r.Host != "" && r.Host != r.URL.Host {
		return false
	}
	switch r.URL.String() {
	case metadataRegion,
		"http://169.254.169.254/opc/v2/identity/cert.pem",
		"http://169.254.169.254/opc/v2/identity/key.pem",
		"http://169.254.169.254/opc/v2/identity/intermediate.pem":
		return r.Method == http.MethodGet && r.Header.Get("Authorization") == "Bearer Oracle"
	default:
		return r.Method == http.MethodPost && r.URL.String() == s.federation
	}
}

func (s *InstanceSigner) SubjectToken(ctx context.Context) (SourceToken, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	sign := s.sign
	if sign == nil {
		sdk, err := s.sdkSigner(ctx)
		if err != nil {
			return SourceToken{}, err
		}
		sign = sdk.Sign
	}
	form := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"scope":                {"urn:opc:idm:__myscopes__"},
		"requested_token_type": {accessTokenType},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(form))
	if err != nil {
		return SourceToken{}, ErrSourceUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	if sign(req) != nil {
		return SourceToken{}, ErrSourceUnavailable
	}
	req.GetBody = nil
	resp, err := s.client.Do(req)
	if err != nil {
		return SourceToken{}, ErrSourceUnavailable
	}
	defer resp.Body.Close()
	var result struct {
		Token string `json:"access_token"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if resp.StatusCode != http.StatusOK || err != nil || len(data) > 65536 || json.Unmarshal(data, &result) != nil {
		return SourceToken{}, ErrSourceUnavailable
	}
	claims, err := decodeClaims(result.Token)
	if err != nil {
		return SourceToken{}, ErrSourceUnavailable
	}
	return SourceToken{Raw: result.Token, Claims: claims}, nil
}

// Claims are inspected locally to constrain the exchange. OpenAI verifies the
// signature of the original, unmodified JWT against the registered issuer.
func decodeClaims(jwt string) (Claims, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 || parts[0] == "" || parts[2] == "" || strings.ContainsAny(jwt, " \t\r\n") {
		return Claims{}, ErrSourceUnavailable
	}
	body, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrSourceUnavailable
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return Claims{}, ErrSourceUnavailable
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		var raw json.RawMessage
		if err != nil || !ok || fields[key] != nil || dec.Decode(&raw) != nil {
			return Claims{}, ErrSourceUnavailable
		}
		fields[key] = raw
	}
	if _, err := dec.Token(); err != nil {
		return Claims{}, ErrSourceUnavailable
	}
	if _, err := dec.Token(); err != io.EOF {
		return Claims{}, ErrSourceUnavailable
	}
	var claims Claims
	for key, dst := range map[string]*string{
		"iss": &claims.Issuer, "sub_type": &claims.SubjectType, "ipst_instance": &claims.InstanceOCID,
		"ipst_compartment": &claims.CompartmentOCID, "domain_id": &claims.DomainID, "ca_ocid": &claims.TenancyOCID,
	} {
		if json.Unmarshal(fields[key], dst) != nil || *dst == "" {
			return Claims{}, ErrSourceUnavailable
		}
	}
	if json.Unmarshal(fields["aud"], &claims.Audience) != nil {
		var aud string
		if json.Unmarshal(fields["aud"], &aud) != nil || aud == "" {
			return Claims{}, ErrSourceUnavailable
		}
		claims.Audience = []string{aud}
	}
	var issued, expires, notBefore int64
	if json.Unmarshal(fields["iat"], &issued) != nil || json.Unmarshal(fields["exp"], &expires) != nil ||
		issued <= 0 || expires <= issued {
		return Claims{}, ErrSourceUnavailable
	}
	if raw, ok := fields["nbf"]; ok && (json.Unmarshal(raw, &notBefore) != nil || notBefore <= 0 || time.Now().Unix() < notBefore) {
		return Claims{}, ErrSourceUnavailable
	}
	claims.IssuedAt, claims.ExpiresAt = time.Unix(issued, 0), time.Unix(expires, 0)
	return claims, nil
}
