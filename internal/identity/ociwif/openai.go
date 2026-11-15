package ociwif

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const (
	tokenExchangeGrant = "urn:ietf:params:oauth:grant-type:token-exchange"
	jwtTokenType       = "urn:ietf:params:oauth:token-type:jwt"
	accessTokenType    = "urn:ietf:params:oauth:token-type:access_token"
	exchangeTimeout    = 15 * time.Second
	maxExchangeBody    = 64 << 10
)

type openAIExchanger struct {
	client *http.Client
}

// NewOpenAIExchanger returns the direct OpenAI WIF adapter. It uses standard TLS
// verification at the fixed endpoint, with no proxy, redirect or replay.
func NewOpenAIExchanger() Exchanger {
	// A fresh HTTP/1 connection for each request avoids the replay paths
	// associated with pooled connections and HTTP/2 streams.
	return &openAIExchanger{client: &http.Client{
		Transport: &http.Transport{
			TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
			TLSNextProto:           map[string]func(string, *tls.Conn) http.RoundTripper{},
			DisableKeepAlives:      true,
			DisableCompression:     true,
			MaxResponseHeaderBytes: maxExchangeBody,
		},
		Timeout: exchangeTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (e *openAIExchanger) Exchange(ctx context.Context, input ExchangeRequest) (TokenResponse, error) {
	if ctx == nil || ctx.Err() != nil {
		return TokenResponse{}, ErrExchangeUnknown
	}
	if input.SourceToken == "" || input.IdentityProvider == "" || input.ServiceAccount == "" {
		return TokenResponse{}, ErrMappingMismatch
	}

	// The documented request has no audience field. OpenAI validates the
	// original audience inside the OCI JWT against its provider configuration.
	body, err := json.Marshal(map[string]string{
		"grant_type":           tokenExchangeGrant,
		"subject_token_type":   jwtTokenType,
		"subject_token":        input.SourceToken,
		"identity_provider_id": input.IdentityProvider,
		"service_account_id":   input.ServiceAccount,
	})
	if err != nil || len(body) > maxExchangeBody {
		return TokenResponse{}, ErrMappingMismatch
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAIExchangeEndpoint, bytes.NewReader(body))
	if err != nil {
		return TokenResponse{}, ErrExchangeUnknown
	}
	req.GetBody = nil
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return TokenResponse{}, ErrExchangeUnknown
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return TokenResponse{}, ErrExchangeUnknown
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxExchangeBody+1))
	if err != nil || len(raw) > maxExchangeBody {
		return TokenResponse{}, ErrExchangeUnknown
	}
	var result struct {
		AccessToken     string `json:"access_token"`
		TokenType       string `json:"token_type"`
		IssuedTokenType string `json:"issued_token_type"`
		ExpiresIn       int64  `json:"expires_in"`
		ExpiresAt       int64  `json:"expires_at"`
	}
	if json.Unmarshal(raw, &result) != nil || !validBearerToken(result.AccessToken) ||
		!isBearer(result.TokenType) || result.IssuedTokenType != accessTokenType {
		return TokenResponse{}, ErrExchangeUnknown
	}
	return TokenResponse(result), nil
}
