// Package providerlifecycle contains single-effect, provider-specific static
// credential adapters for an ALREADY ADMITTED K10 trusted host. These functions
// must be called only under rotation.Prepare/Finalize + the existing journal and
// an external Goal Writer fence. No CLI calls or provider operations occur by default.
package providerlifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var accountID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)
var generationName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,90}_GEN_[1-9][0-9]*$`)
var ociID = regexp.MustCompile(`^ocid1\.[a-z]+\.[A-Za-z0-9._-]{3,256}$`)
var regionID = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[1-9]$`)

// HostAuthorize MUST be a trusted callback bound to this operation, provider,
// immutable target, and current Goal Claim. A callback created from a user- or
// model-supplied JSON file does not confer authority.
type HostAuthorize func(context.Context, string, string, string) error

// Authenticate is supplied by a separately privileged provider host.
// OCI must sign the supplied method, URL, host and body and preserve the fixed
// headers. It may set Authorization, Date and X-Date, and restore an identical
// readable body after reading it.
type Authenticate func(*http.Request) error

type Client struct {
	http *http.Client
	sign Authenticate
}

func NewClient(sign Authenticate, transport http.RoundTripper) (Client, error) {
	if sign == nil {
		return Client{}, errors.New("provider authentication required")
	}
	if transport == nil {
		transport = &http.Transport{
			Proxy: nil, DialContext: (&net.Dialer{Timeout: 8 * time.Second}).DialContext,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			TLSNextProto:    map[string]func(string, *tls.Conn) http.RoundTripper{}, TLSHandshakeTimeout: 8 * time.Second,
		}
	}
	return Client{http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, sign: sign}, nil
}

func (c Client) call(ctx context.Context, method, origin, path string, body []byte, expected ...int) ([]byte, error) {
	return c.callHeaders(ctx, method, origin, path, body, nil, expected...)
}

func (c Client) callHeaders(ctx context.Context, method, origin, path string, body []byte, headers map[string]string, expected ...int) ([]byte, error) {
	return c.callBeforeSend(ctx, method, origin, path, body, headers, nil, expected...)
}

func (c Client) callBeforeSend(ctx context.Context, method, origin, path string, body []byte, headers map[string]string, beforeSend func(context.Context) error, expected ...int) ([]byte, error) {
	if c.http == nil || c.sign == nil || ctx.Err() != nil {
		return nil, errors.New("provider mutation client unavailable")
	}
	u, err := url.Parse(origin + path)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.Port() != "" || u.RawQuery != "" || u.Host == "" {
		return nil, errors.New("invalid immutable provider origin")
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, errors.New("cannot build provider effect")
	}
	req.GetBody = nil // Never let http.Client transparently replay writes.
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	initialBody := req.Body
	if err := c.sign(req); err != nil {
		return nil, errors.New("provider signer refused operation")
	}
	if req.Method != method || req.URL == nil || req.URL.String() != u.String() || (req.Host != "" && req.Host != u.Host) || req.Body != initialBody || req.GetBody != nil {
		return nil, errors.New("provider signer mutated immutable effect")
	}
	if beforeSend != nil {
		if err := beforeSend(ctx); err != nil {
			return nil, err
		}
	}
	return c.send(req, expected...)
}

func (c Client) send(req *http.Request, expected ...int) ([]byte, error) {
	res, err := c.http.Do(req)
	if err != nil {
		return nil, errors.New("provider effect response unknown")
	}
	defer res.Body.Close()
	accepted := false
	for _, code := range expected {
		if res.StatusCode == code {
			accepted = true
		}
	}
	if !accepted {
		return nil, fmt.Errorf("provider effect response unknown (HTTP %d)", res.StatusCode)
	}
	if res.StatusCode == http.StatusNoContent {
		return nil, nil
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 131073))
	if err != nil || len(b) > 131072 {
		return nil, errors.New("provider acknowledgement invalid")
	}
	return b, nil
}

// Receipt describes a provider acknowledgement, never an authenticated
// consumer use or confirmed deletion. No secret values or key fingerprints.
type Receipt struct {
	Provider             string `json:"provider"`
	Action               string `json:"action"`
	Resource             string `json:"resource"`
	CredentialID         string `json:"credential_id"`
	Status               string `json:"status"`
	ProofDigest          string `json:"proof_digest"`
	AuthorizesRevocation bool   `json:"authorizes_revocation"`
}

func receipt(provider, action, resource, id string) Receipt {
	p := Receipt{Provider: provider, Action: action, Resource: resource, CredentialID: id, Status: "ACKNOWLEDGED_UNVERIFIED"}
	data, _ := json.Marshal(p)
	sum := sha256.Sum256(data)
	p.ProofDigest = hex.EncodeToString(sum[:])
	return p
}
func authorize(ctx context.Context, cb HostAuthorize, provider, op, target string) error {
	if cb == nil || ctx.Err() != nil || target == "" {
		return errors.New("trusted effect admission required")
	}
	if err := cb(ctx, provider, op, target); err != nil {
		return errors.New("trusted provider effect rejected")
	}
	return ctx.Err()
}
func validRepo(repo string) bool {
	parts := strings.Split(repo, "/")
	return len(parts) == 2 && segment.MatchString(parts[0]) && segment.MatchString(parts[1]) && parts[0] != "." && parts[1] != "." && parts[1] != ".."
}
func validOCI(s, kind string) bool {
	return ociID.MatchString(s) && strings.HasPrefix(s, "ocid1."+kind+".")
}
