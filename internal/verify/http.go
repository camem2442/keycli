package verify

import (
	"fmt"
	"net/http"
	"os"
	"strings"
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

// Reject stale or hostile test endpoints instead of silently contacting production.
func requireOfficialOrigin(key, expected string) error {
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" && strings.TrimRight(value, "/") != expected {
		return fmt.Errorf("%s must be the official verification endpoint", key)
	}
	return nil
}
