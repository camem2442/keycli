package gh

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.org/x/crypto/nacl/box"
)

const (
	githubRESTOrigin      = "https://api.github.com"
	githubRESTVersion     = "2026-03-10"
	githubRESTTimeout     = 30 * time.Second
	maxGitHubRESTResponse = 2 << 20
	maxGitHubSecretBytes  = 48 << 10
)

var (
	githubOwnerName        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
	githubRepoName         = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	githubEnvironment      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	githubSecretName       = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	githubGenerationSecret = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*_GEN_[1-9][0-9]*$`)
	errTargetIdentity      = errors.New("GitHub Secret target identity did not match the approved scope")
)

// SecretRESTTarget combines the requested Actions Secret scope and immutable
// identities supplied by the trusted host. The adapter does not grant authority.
type SecretRESTTarget struct {
	Scope, Repository, Environment, Organization, Destination      string
	RepositoryID, RepositoryOwnerID, EnvironmentID, OrganizationID int64
	SelectedRepositoryIDs                                          []int64
}

type SecretWriteOutcome string

const (
	SecretNotWritten        SecretWriteOutcome = "NOT_WRITTEN"
	SecretWriteAcknowledged SecretWriteOutcome = "ACKNOWLEDGED"
	SecretWriteUnknown      SecretWriteOutcome = "UNKNOWN_EFFECT"
)

// SecretRESTClient is the direct GitHub Actions Secret adapter for a trusted
// host. The host must verify authorization and persist a write intent first.
type SecretRESTClient struct {
	client    *http.Client
	authorize func(*http.Request)
}

// NewSecretRESTClient constructs a direct HTTPS client with no proxy or
// redirects. It does not discover or load credentials.
func NewSecretRESTClient(token string) (*SecretRESTClient, error) {
	return newSecretRESTClient(token, &http.Transport{
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     30 * time.Second,
	})
}

func newSecretRESTClient(token string, transport http.RoundTripper) (*SecretRESTClient, error) {
	if token == "" || len(token) > 8192 || strings.ContainsFunc(token, func(r rune) bool {
		return r < '!' || r > '~'
	}) {
		return nil, errors.New("invalid GitHub REST credential")
	}
	return &SecretRESTClient{
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		authorize: func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+token)
		},
	}, nil
}

// VerifyTarget performs a read-only identity check. Its result is a point-in-time
// observation; GitHub's Secret endpoints do not expose an atomic identity CAS.
func (c *SecretRESTClient) VerifyTarget(ctx context.Context, target SecretRESTTarget) error {
	if _, err := target.secretPath(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, githubRESTTimeout)
	defer cancel()
	return c.verifyTarget(ctx, target)
}

// PutSecret borrows secret without modifying it. The caller owns cleanup of
// those bytes; the adapter clears its own copy after use.
func (c *SecretRESTClient) PutSecret(ctx context.Context, target SecretRESTTarget, secret []byte) (SecretWriteOutcome, error) {
	if !validGitHubSecretSize(secret) {
		return SecretNotWritten, errors.New("GitHub Actions Secret must contain 1 to 48 KiB of bytes")
	}
	return c.PutSecretFrom(ctx, target, func(context.Context) ([]byte, error) {
		return slices.Clone(secret), nil
	})
}

// PutSecretFrom attempts one provider write. It invokes load only after the
// approved identities and public key have been checked. Ownership of the
// returned slice transfers to the adapter, which clears it even on failure.
// ACKNOWLEDGED reflects GitHub's response only. The host must reconcile any
// unknown effect before deciding whether another write is permitted.
func (c *SecretRESTClient) PutSecretFrom(ctx context.Context, target SecretRESTTarget, load func(context.Context) ([]byte, error)) (SecretWriteOutcome, error) {
	return c.putSecretFrom(ctx, target, load, nil)
}

// CreateRepositoryGenerationFrom checks for an absent generation immediately
// before the final authorization and PUT. The host serializes its writers.
func (c *SecretRESTClient) CreateRepositoryGenerationFrom(ctx context.Context, target SecretRESTTarget, load func(context.Context) ([]byte, error), beforePut func(context.Context) error) (SecretWriteOutcome, error) {
	if target.Scope != "repository" || !githubGenerationSecret.MatchString(target.Destination) || beforePut == nil {
		return SecretNotWritten, errTargetIdentity
	}
	return c.putSecretFrom(ctx, target, load, func(ctx context.Context, path string) error {
		if err := c.verifyTarget(ctx, target); err != nil {
			return err
		}
		resp, err := c.request(ctx, http.MethodGet, path+"/"+target.Destination, nil)
		if err != nil {
			return errors.New("GitHub Secret destination unavailable")
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			return errors.New("GitHub Secret destination absence not established")
		}
		return beforePut(ctx)
	})
}

func (c *SecretRESTClient) putSecretFrom(ctx context.Context, target SecretRESTTarget, load func(context.Context) ([]byte, error), beforeWrite func(context.Context, string) error) (SecretWriteOutcome, error) {
	if load == nil {
		return SecretNotWritten, errors.New("GitHub Secret loader is unavailable")
	}
	path, err := target.secretPath()
	if err != nil {
		return SecretNotWritten, err
	}
	ctx, cancel := context.WithTimeout(ctx, githubRESTTimeout)
	defer cancel()
	if err := c.verifyTarget(ctx, target); err != nil {
		return SecretNotWritten, err
	}

	var key struct {
		ID  string `json:"key_id"`
		Key string `json:"key"`
	}
	if err := c.getJSON(ctx, path+"/public-key", &key); err != nil {
		return SecretNotWritten, err
	}
	publicKey, err := decodeGitHubPublicKey(key.ID, key.Key)
	if err != nil {
		return SecretNotWritten, err
	}
	if ctx.Err() != nil {
		return SecretNotWritten, errors.New("GitHub Secret preflight expired before value lookup")
	}
	secret, err := load(ctx)
	defer clear(secret)
	if err != nil {
		return SecretNotWritten, errors.New("cannot load GitHub Actions Secret")
	}
	if !validGitHubSecretSize(secret) {
		return SecretNotWritten, errors.New("GitHub Actions Secret must contain 1 to 48 KiB of bytes")
	}
	sealed, err := box.SealAnonymous(nil, secret, &publicKey, rand.Reader)
	if err != nil {
		return SecretNotWritten, errors.New("cannot encrypt GitHub Actions Secret")
	}
	payload := struct {
		EncryptedValue        string  `json:"encrypted_value"`
		KeyID                 string  `json:"key_id"`
		Visibility            string  `json:"visibility,omitempty"`
		SelectedRepositoryIDs []int64 `json:"selected_repository_ids,omitempty"`
	}{EncryptedValue: base64.StdEncoding.EncodeToString(sealed), KeyID: key.ID}
	if target.Scope == "organization" {
		payload.Visibility = "selected"
		payload.SelectedRepositoryIDs = slices.Clone(target.SelectedRepositoryIDs)
		slices.Sort(payload.SelectedRepositoryIDs)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return SecretNotWritten, errors.New("cannot encode GitHub Actions Secret request")
	}
	if beforeWrite != nil {
		if err := beforeWrite(ctx, path); err != nil {
			return SecretNotWritten, err
		}
	}
	if ctx.Err() != nil {
		return SecretNotWritten, errors.New("GitHub Secret preflight expired before write")
	}

	resp, err := c.request(ctx, http.MethodPut, path+"/"+target.Destination, body)
	if err != nil {
		return SecretWriteUnknown, errors.New("GitHub Secret write acknowledgement unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && (beforeWrite != nil || resp.StatusCode != http.StatusNoContent) {
		return SecretWriteUnknown, fmt.Errorf("GitHub Secret write acknowledgement unavailable (HTTP %d)", resp.StatusCode)
	}
	return SecretWriteAcknowledged, nil
}

func validGitHubSecretSize(secret []byte) bool {
	return len(secret) > 0 && len(secret) <= maxGitHubSecretBytes
}

func (t SecretRESTTarget) secretPath() (string, error) {
	if !githubSecretName.MatchString(t.Destination) || strings.HasPrefix(strings.ToUpper(t.Destination), "GITHUB_") {
		return "", errTargetIdentity
	}
	if t.Scope == "organization" {
		if t.Repository != "" || t.RepositoryID != 0 || t.RepositoryOwnerID != 0 ||
			t.Environment != "" || t.EnvironmentID != 0 ||
			!githubOwnerName.MatchString(t.Organization) || t.OrganizationID <= 0 ||
			!validSelectedRepositories(t.SelectedRepositoryIDs) {
			return "", errTargetIdentity
		}
		return "/orgs/" + t.Organization + "/actions/secrets", nil
	}
	owner, repo, found := strings.Cut(t.Repository, "/")
	if !found || !githubOwnerName.MatchString(owner) || !githubRepoName.MatchString(repo) ||
		repo == "." || repo == ".." || t.RepositoryID <= 0 || t.RepositoryOwnerID <= 0 ||
		t.Organization != "" || t.OrganizationID != 0 || len(t.SelectedRepositoryIDs) != 0 {
		return "", errTargetIdentity
	}
	path := "/repos/" + t.Repository
	switch t.Scope {
	case "repository":
		if t.Environment == "" && t.EnvironmentID == 0 {
			return path + "/actions/secrets", nil
		}
	case "environment":
		if githubEnvironment.MatchString(t.Environment) && t.EnvironmentID > 0 {
			return path + "/environments/" + t.Environment + "/secrets", nil
		}
	}
	return "", errTargetIdentity
}

func decodeGitHubPublicKey(id, encoded string) ([32]byte, error) {
	var key [32]byte
	if len(id) == 0 || len(id) > 128 || strings.ContainsFunc(id, func(r rune) bool {
		return r < '!' || r > '~'
	}) || strings.ContainsAny(encoded, "\r\n") {
		return key, errors.New("invalid GitHub Actions Secret public key")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawStdEncoding.Strict().DecodeString(encoded)
	}
	if err != nil || len(decoded) != len(key) {
		return key, errors.New("invalid GitHub Actions Secret public key")
	}
	// Reject low-order Curve25519 points before passing them to the sealed box.
	if _, err := curve25519.X25519(curve25519.Basepoint, decoded); err != nil {
		return key, errors.New("invalid GitHub Actions Secret public key")
	}
	copy(key[:], decoded)
	return key, nil
}

func (c *SecretRESTClient) request(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	if c == nil || c.client == nil || c.authorize == nil {
		return nil, errors.New("GitHub REST client is unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, method, githubRESTOrigin+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot construct GitHub REST request")
	}
	c.authorize(req)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubRESTVersion)
	if method == http.MethodPut {
		req.Header.Set("Content-Type", "application/json")
		// The host, not net/http, owns the decision to replay a write.
		req.GetBody = nil
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, errors.New("GitHub REST transport failed")
	}
	return resp, nil
}

func (c *SecretRESTClient) getJSON(ctx context.Context, path string, out any) error {
	resp, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub Secret preflight failed (HTTP %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxGitHubRESTResponse+1))
	if err != nil || len(data) > maxGitHubRESTResponse || json.Unmarshal(data, out) != nil {
		return errors.New("invalid GitHub Secret preflight response")
	}
	return nil
}
