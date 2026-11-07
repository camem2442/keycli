package verify

import (
	"net/http"
	"time"
)

// verificationClient never forwards a bearer token through an HTTP redirect.
// The injectable transport is an internal testing boundary, not a URL setting.
func verificationClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &copy
}
