package githubwif

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const exchangeURL = "https://auth.openai.com/oauth/token"
const accessTokenType = "urn:ietf:params:oauth:token-type:access_token"

type Mapping struct {
	ProviderID, ServiceAccountID    string
	RepositoryID, RepositoryOwnerID int64
	WorkflowSHA                     string
}

// SDKExchange is registered by the trusted host only after checking SDK support.
// The SDK callback must exchange the provided subject for the exact mapping.
type SDKExchange func(context.Context, Mapping, string) (TokenResponse, error)

type TokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	IssuedTokenType string `json:"issued_token_type"`
	ExpiresIn       int64  `json:"expires_in"`
	ExpiresAt       int64  `json:"expires_at"`
}

func (TokenResponse) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "[redacted WIF response]") }

type Credential struct {
	token   string
	expires time.Time
}

func (Credential) Format(s fmt.State, _ rune) { _, _ = io.WriteString(s, "[redacted WIF credential]") }
func (c Credential) ExpiresAt() time.Time     { return c.expires }

// Authorize attaches the access token only to the public OpenAI API origin.
// Callers must use a transport that disables redirects and credential/header
// logging for authenticated requests.
func (c Credential) Authorize(req *http.Request) error {
	if req == nil || req.URL == nil || req.URL.Scheme != "https" ||
		req.URL.Host != "api.openai.com" || req.URL.User != nil ||
		(req.Host != "" && req.Host != req.URL.Host) || req.Header.Get("Authorization") != "" ||
		c.token == "" || !time.Now().Before(c.expires) {
		return errors.New("WIF credential cannot authorize this request")
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return nil
}

type Exchange struct {
	contract                 Contract
	mapping                  Mapping
	requestURL, requestToken string
	http                     *http.Client
	sdk                      SDKExchange
	gate                     chan struct{}
	cached                   Credential
}

func (*Exchange) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "[redacted GitHub WIF exchange]")
}

// NewExchange takes GitHub's job-scoped OIDC endpoint/token from the admitted
// host. A registered SDK is selected once. Otherwise the documented HTTP
// exchange is used. Errors never switch transport or credential type.
func NewExchange(contract Contract, mapping Mapping, requestURL, requestToken string, sdk SDKExchange) (*Exchange, error) {
	for _, value := range []string{contract.Audience, contract.Repository, contract.Ref, contract.WorkflowRef,
		contract.Environment, mapping.ProviderID, mapping.ServiceAccountID, requestToken} {
		if !safeToken(value) {
			return nil, errors.New("invalid WIF binding")
		}
	}
	if _, err := hex.DecodeString(mapping.WorkflowSHA); err != nil || len(mapping.WorkflowSHA) != 40 || mapping.RepositoryID <= 0 || mapping.RepositoryOwnerID <= 0 {
		return nil, errors.New("immutable GitHub identity is required")
	}
	u, err := url.Parse(requestURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" ||
		u.Host != u.Hostname() || !strings.HasSuffix(u.Host, ".actions.githubusercontent.com") {
		return nil, errors.New("invalid GitHub Actions OIDC origin")
	}
	query := u.Query()
	query.Set("audience", contract.Audience)
	u.RawQuery = query.Encode()
	httpClient := &http.Client{
		Transport: &http.Transport{
			// HTTP/2 debug can log authorization headers.
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			TLSNextProto:    map[string]func(string, *tls.Conn) http.RoundTripper{},
		},
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &Exchange{
		contract: contract, mapping: mapping, requestURL: u.String(), requestToken: requestToken,
		http: httpClient, sdk: sdk, gate: make(chan struct{}, 1),
	}, nil
}

func (e *Exchange) Route() string {
	if e.sdk != nil {
		return "sdk"
	}
	return "direct"
}

// Get shares a successful refresh across callers. Cached tokens stay in
// memory and are refreshed before expiry without crossing transport routes.
func (e *Exchange) Get(ctx context.Context) (Credential, error) {
	if e == nil || e.gate == nil {
		return Credential{}, errors.New("WIF exchange is not configured")
	}
	select {
	case <-ctx.Done():
		return Credential{}, ctx.Err()
	case e.gate <- struct{}{}:
	}
	defer func() { <-e.gate }()
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	if time.Until(e.cached.expires) > 30*time.Second {
		return e.cached, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var oidc struct {
		Value string `json:"value"`
	}
	if err := e.request(ctx, http.MethodGet, e.requestURL, e.requestToken, nil, &oidc); err != nil {
		return Credential{}, fmt.Errorf("GitHub OIDC acquisition: %w", err)
	}
	exp, err := e.contract.checkSubject(oidc.Value, e.mapping, time.Now())
	if err != nil {
		return Credential{}, err
	}
	var result TokenResponse
	if e.sdk != nil {
		result, err = e.sdk(ctx, e.mapping, oidc.Value)
		if err != nil {
			return Credential{}, errors.New("OpenAI SDK WIF exchange failed")
		}
	} else {
		body, err := json.Marshal(map[string]string{
			"grant_type":           "urn:ietf:params:oauth:grant-type:token-exchange",
			"subject_token_type":   "urn:ietf:params:oauth:token-type:jwt",
			"subject_token":        oidc.Value,
			"identity_provider_id": e.mapping.ProviderID,
			"service_account_id":   e.mapping.ServiceAccountID,
		})
		if err != nil {
			return Credential{}, errors.New("cannot encode WIF exchange")
		}
		if err := e.request(ctx, http.MethodPost, exchangeURL, "", body, &result); err != nil {
			return Credential{}, fmt.Errorf("OpenAI WIF exchange: %w", err)
		}
	}
	now := time.Now()
	expires := time.Unix(result.ExpiresAt, 0)
	if !safeToken(result.AccessToken) || !strings.EqualFold(result.TokenType, "Bearer") ||
		result.IssuedTokenType != accessTokenType || result.ExpiresIn <= 0 || result.ExpiresIn > 3600 ||
		!expires.After(now) || expires.After(exp) || expires.After(now.Add(time.Duration(result.ExpiresIn)*time.Second)) {
		return Credential{}, errors.New("invalid OpenAI WIF token response")
	}
	e.cached = Credential{token: result.AccessToken, expires: expires}
	return e.cached, nil
}

func (e *Exchange) request(ctx context.Context, method, endpoint, token string, body []byte, dst any) error {
	var reader io.Reader
	if body != nil {
		// Avoid GetBody so the client cannot replay a credential POST.
		reader = io.NopCloser(bytes.NewReader(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return errors.New("invalid identity request")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return errors.New("identity request transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("identity request returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(data) > 65536 || json.Unmarshal(data, dst) != nil {
		return errors.New("invalid identity response")
	}
	return nil
}

func safeToken(value string) bool {
	if value == "" || len(value) > 16384 {
		return false
	}
	for _, ch := range value {
		if ch < 33 || ch > 126 {
			return false
		}
	}
	return true
}

// The local comparison constrains the exchange. OpenAI validates signature,
// issuer and mapping. A decoded claim payload alone carries no authority.
func (p Contract) checkSubject(token string, mapping Mapping, now time.Time) (time.Time, error) {
	invalid := errors.New("GitHub OIDC subject does not match the WIF binding")
	parts := strings.Split(token, ".")
	if !safeToken(token) || len(parts) != 3 || parts[0] == "" || parts[2] == "" {
		return time.Time{}, invalid
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return time.Time{}, invalid
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') {
		return time.Time{}, invalid
	}
	claims := map[string]json.RawMessage{}
	for dec.More() {
		key, err := dec.Token()
		name, ok := key.(string)
		if err != nil || !ok || claims[name] != nil {
			return time.Time{}, invalid
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return time.Time{}, invalid
		}
		claims[name] = value
	}
	if _, err := dec.Token(); err != nil {
		return time.Time{}, invalid
	}
	if _, err := dec.Token(); err != io.EOF {
		return time.Time{}, invalid
	}
	for key, want := range map[string]string{
		"iss": Issuer, "aud": p.Audience, "repository": p.Repository, "ref": p.Ref,
		"workflow_ref": p.WorkflowRef, "environment": p.Environment, "event_name": probeEvent,
		"repository_id":       strconv.FormatInt(mapping.RepositoryID, 10),
		"repository_owner_id": strconv.FormatInt(mapping.RepositoryOwnerID, 10),
		"workflow_sha":        mapping.WorkflowSHA,
	} {
		var value string
		if json.Unmarshal(claims[key], &value) != nil || want == "" || value != want {
			return time.Time{}, invalid
		}
	}
	if raw, exists := claims["job_workflow_ref"]; exists {
		var ref string
		if json.Unmarshal(raw, &ref) != nil || ref != p.WorkflowRef {
			return time.Time{}, invalid
		}
	}
	if raw, exists := claims["job_workflow_sha"]; exists {
		var sha string
		if json.Unmarshal(raw, &sha) != nil || sha != mapping.WorkflowSHA {
			return time.Time{}, invalid
		}
	}
	var expires, issued int64
	var subject string
	if json.Unmarshal(claims["exp"], &expires) != nil || json.Unmarshal(claims["iat"], &issued) != nil ||
		json.Unmarshal(claims["sub"], &subject) != nil || subject == "" ||
		issued <= 0 || issued > now.Unix()+30 || expires <= now.Unix() || expires <= issued {
		return time.Time{}, invalid
	}
	return time.Unix(expires, 0), nil
}
