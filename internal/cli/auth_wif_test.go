package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWIFCheckRegistration(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "registration.json")
	for _, tc := range []struct {
		name, model string
		mode        os.FileMode
		deadline    time.Duration
		change      bool
		ok          bool
	}{
		{"approved", "fixture-model", 0600, time.Hour, false, true},
		{"unapproved bytes", "fixture-model", 0600, time.Hour, true, false},
		{"unsafe mode", "fixture-model", 0644, time.Hour, false, false},
		{"expired approval", "fixture-model", 0600, -time.Hour, false, false},
		{"path injection", "../other", 0600, time.Hour, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(fmt.Sprintf(`{"v":1,"delivery_toml":"fixture","model":%q,"provider":"oci-instance-wif","binding":{},"approved_until":%q}`,
				tc.model, time.Now().Add(tc.deadline).UTC().Format(time.RFC3339)))
			digest := fmt.Sprintf("%x", sha256.Sum256(data))
			if tc.change {
				data = append(data, '\n')
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			reg, err := readWIFCheckRegistration(path, digest)
			if (err == nil) != tc.ok || (tc.ok && reg.Digest != digest) {
				t.Fatalf("unexpected registration result: %v", err)
			}
		})
	}
}

type wifTestCredential struct{ expiry time.Time }

func (c wifTestCredential) ExpiresAt() time.Time { return c.expiry }
func (c wifTestCredential) Authorize(req *http.Request) error {
	req.Header.Set("Authorization", "Bearer fixture-secret-marker")
	return nil
}

type wifTestTransport func(*http.Request) (*http.Response, error)

func (fn wifTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestWIFCheckModel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		expiry time.Duration
		ok     bool
	}{
		{"success", 200, `{"id":"fixture-model","object":"model","extra":"fixture-secret-marker"}`, time.Minute, true},
		{"wrong model", 200, `{"id":"wrong","object":"model"}`, time.Minute, false},
		{"redirect", 307, "fixture-secret-marker", time.Minute, false},
		{"forbidden", 403, "fixture-secret-marker", time.Minute, false},
		{"expired", 200, `{"id":"fixture-model","object":"model"}`, -time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newWIFModelClient()
			calls := 0
			client.Transport = wifTestTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Method != http.MethodGet || req.URL.String() != "https://api.openai.com/v1/models/fixture-model" ||
					req.Header.Get("Authorization") != "Bearer fixture-secret-marker" {
					t.Fatal("unexpected model request")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://other.invalid"}},
					Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			reg := wifCheckRegistration{Provider: "github-actions-wif", Model: "fixture-model", Digest: strings.Repeat("a", 64)}
			var out bytes.Buffer
			err := checkWIFModel(context.Background(), client, reg, wifTestCredential{time.Now().Add(tc.expiry)}, &out)
			if calls != 1 || (err == nil) != tc.ok || strings.Contains(out.String(), "fixture-secret-marker") ||
				(err != nil && (out.Len() != 0 || strings.Contains(err.Error(), "fixture-secret-marker"))) ||
				(tc.ok && !strings.Contains(out.String(), reg.Digest)) {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
