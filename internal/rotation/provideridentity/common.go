// Package provideridentity contains read-only, exact-target metadata observers
// for the separately authorised K10-04 revocation workflow. It never revokes,
// retrieves credential values, or decides authority. A trusted host owns the
// authentication callback, expected identities and final revocation grant.
package provideridentity

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/rotation"
)

const maxReadBytes = 1 << 20

// Authenticate is installed exclusively by a trusted host. For Oracle, this
// callback signs a fully constructed, exactly scoped OCI management GET.
type Authenticate func(*http.Request) error

// Reader always uses a literal HTTPS endpoint and disables redirects, ambient
// proxies and HTTP/2 debug header logging. Inject Transport only in trusted
// host tests; no user-supplied URL or ordinary KeyCLI CLI option controls it.
type Reader struct {
	client *http.Client
	auth   Authenticate
}

func NewReader(auth Authenticate, transport http.RoundTripper) (Reader, error) {
	if auth == nil {
		return Reader{}, errors.New("trusted provider read authentication required")
	}
	if transport == nil {
		transport = &http.Transport{
			Proxy:               nil,
			DialContext:         (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
			TLSHandshakeTimeout: 8 * time.Second,
			ForceAttemptHTTP2:   false,
		}
	}
	return Reader{client: &http.Client{Transport: transport, Timeout: 15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, auth: auth}, nil
}

func (r Reader) read(ctx context.Context, origin, path string, out any, headers map[string]string) error {
	return r.readWithResponseHeaders(ctx, origin, path, out, headers, nil)
}

func (r Reader) readWithResponseHeaders(ctx context.Context, origin, path string, out any, headers map[string]string, responseHeaders *http.Header) error {
	if r.client == nil || r.auth == nil || ctx.Err() != nil {
		return errors.New("trusted provider reader unavailable")
	}
	u, err := url.Parse(origin + path)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" || u.Host == "" {
		return errors.New("invalid provider read target")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("invalid provider GET request")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if err := r.auth(req); err != nil {
		return errors.New("provider authentication rejected")
	}
	// A signer must not redirect or alter the fixed GET target. OCI's
	// signer may add headers, but neither host nor URL is negotiable.
	if req.Method != http.MethodGet || req.URL.String() != u.String() ||
		(req.Host != "" && req.Host != u.Host) || req.Body != nil {
		return errors.New("authentication mutated provider GET target")
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return errors.New("provider identity GET transport failure")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("provider identity GET rejected or unavailable")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReadBytes+1))
	if err != nil || len(body) > maxReadBytes {
		return errors.New("unavailable or oversized provider metadata")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return errors.New("invalid provider metadata JSON")
	}
	if responseHeaders != nil {
		*responseHeaders = resp.Header.Clone()
	}
	return nil
}

func shaMetadata(meta any) string {
	b, _ := json.Marshal(meta)
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:])
}
func fixedTime(s string) (time.Time, error) {
	t, e := time.Parse(time.RFC3339Nano, s)
	if e != nil || t.IsZero() {
		return time.Time{}, errors.New("invalid pinned provider timestamp")
	}
	return t.UTC(), nil
}
func eqTime(a, b string) bool {
	x, e := fixedTime(a)
	if e != nil {
		return false
	}
	y, e := fixedTime(b)
	return e == nil && x.Equal(y)
}
func exactPart(s string) bool {
	if s == "" || len(s) > 256 || strings.ContainsAny(s, "/\\?#% ") {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func noWildcard(s string) bool { return s != "" && !strings.ContainsAny(s, "*?[]{}") }

// Reject caller-edited plan contents before touching a provider. The trusted
// revocation host must still independently authorise the exact operation.
func validPlan(p rotation.PreparationPlan) bool {
	if p.Version != 1 || p.ID == "" {
		return false
	}
	rebuilt, err := rotation.NewPreparationPlan(p.Old, p.New, p.Consumers)
	return err == nil && rebuilt.ID == p.ID
}
