package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultCFAPIBase = "https://api.cloudflare.com/client/v4"
const defaultCFAccountID = "b398955bcfe917a4af61d179af967b6a"

type cfVerifyResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

type cfAccountsResponse struct {
	Success bool              `json:"success"`
	Result  []json.RawMessage `json:"result"`
	Errors  []struct {
		Code int `json:"code"`
	} `json:"errors"`
}

type cfHTTP struct {
	base    string
	account string
	client  *http.Client
}

func newCFHTTP(client *http.Client) *cfHTTP {
	base := strings.TrimRight(os.Getenv("CF_API_BASE"), "/")
	if base == "" {
		base = defaultCFAPIBase
	}
	acct := strings.TrimSpace(os.Getenv("CF_ACCOUNT_ID"))
	if acct == "" {
		acct = defaultCFAccountID
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &cfHTTP{base: base, account: acct, client: client}
}

func (c *cfHTTP) bearerRequest(ctx context.Context, method, path string, token []byte) (*http.Response, []byte, error) {
	url := c.base + path
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if err != nil {
		return resp, nil, err
	}
	return resp, body, nil
}

func tokenVerify(ctx context.Context, c *cfHTTP, token []byte) (status int, codes []int, ok bool, err error) {
	resp, body, err := c.bearerRequest(ctx, http.MethodGet, "/user/tokens/verify", token)
	if err != nil {
		return 0, nil, false, err
	}
	status = resp.StatusCode
	var parsed cfVerifyResponse
	_ = json.Unmarshal(body, &parsed)
	for _, e := range parsed.Errors {
		codes = append(codes, e.Code)
	}
	ok = status == http.StatusOK && parsed.Success
	return status, codes, ok, nil
}

func accountsCount(ctx context.Context, c *cfHTTP, token []byte) (int, error) {
	resp, body, err := c.bearerRequest(ctx, http.MethodGet, "/accounts", token)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("accounts http %d", resp.StatusCode)
	}
	var parsed cfAccountsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return 0, err
	}
	if !parsed.Success {
		return 0, fmt.Errorf("accounts api not successful")
	}
	return len(parsed.Result), nil
}
