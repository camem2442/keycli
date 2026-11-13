package providerlifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

const oracleFixtureSecret = "ocid1.vaultsecret.oc1.ap-osaka-1.synthetic"

func TestOracleNamedPendingStageUsesDetachedSignedInputs(t *testing.T) {
	const etag = `"fixture-tag"`
	wantBody := []byte(`{"secretContent":{"content":"dGVzdC1vbmx5","contentType":"BASE64","name":"kc-op-123","stage":"PENDING"}}`)
	var order []string
	var signed *http.Request
	signer := func(req *http.Request) error {
		order = append(order, "sign")
		signed = req
		body, err := io.ReadAll(req.Body)
		if err != nil || !bytes.Equal(body, wantBody) || req.Header.Get("If-Match") != etag {
			t.Fatal("wrong signer inputs")
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.Header.Set("Authorization", "synthetic-signature")
		return nil
	}
	client, err := NewClient(signer, roundTrip(func(req *http.Request) (*http.Response, error) {
		order = append(order, "send")
		body, err := io.ReadAll(req.Body)
		sum := sha256.Sum256(wantBody)
		if err != nil || !bytes.Equal(body, wantBody) || req.GetBody != nil || req.ContentLength != int64(len(wantBody)) ||
			req.Method != http.MethodPut || req.URL.String() != "https://vaults.ap-osaka-1.oci.oraclecloud.com/20180608/secrets/"+oracleFixtureSecret ||
			req.Host != "vaults.ap-osaka-1.oci.oraclecloud.com" || req.Header.Get("If-Match") != etag ||
			req.Header.Get("Authorization") != "synthetic-signature" || req.Header.Get("X-Content-Sha256") != base64.StdEncoding.EncodeToString(sum[:]) {
			t.Fatal("wrong detached request")
		}
		return response(http.StatusOK, fmt.Sprintf(`{"id":%q,"lifecycleState":"ACTIVE"}`, oracleFixtureSecret)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	gate := func(_ context.Context, provider, action, target string) error {
		order = append(order, "admit")
		if provider != "oracle" || action != oraclePendingAction || target != "ap-osaka-1/"+oracleFixtureSecret {
			t.Fatal("wrong admission scope")
		}
		return nil
	}
	fence := func(context.Context) error {
		order = append(order, "fence")
		signed.URL.Host = "unrelated.invalid"
		signed.Header.Set("If-Match", "*")
		return nil
	}
	receipt, err := client.StageOraclePendingVaultVersionWithFence(context.Background(), gate, "ap-osaka-1", oracleFixtureSecret, etag, "kc-op-123", []byte("test-only"), fence)
	if err != nil || receipt.Status != "ACKNOWLEDGED_UNVERIFIED" || receipt.AuthorizesRevocation ||
		!slices.Equal(order, []string{"admit", "sign", "fence", "send"}) {
		t.Fatalf("unexpected stage result %v, %v, %v", receipt, err, order)
	}
}

type oracleBodyWithClose struct {
	io.Reader
	onClose func()
}

func (r oracleBodyWithClose) Close() error {
	r.onClose()
	return nil
}

func TestOracleSignerCannotChangeDeletionRequest(t *testing.T) {
	cases := map[string]func(*http.Request){
		"method":           func(r *http.Request) { r.Method = http.MethodDelete },
		"url":              func(r *http.Request) { r.URL.Path += "/different" },
		"nil URL":          func(r *http.Request) { r.URL = nil },
		"host":             func(r *http.Request) { r.Host = "" },
		"body":             func(r *http.Request) { r.Body = io.NopCloser(strings.NewReader(`{}`)) },
		"content length":   func(r *http.Request) { r.ContentLength++ },
		"wire length":      func(r *http.Request) { r.Header.Set("Content-Length", "0") },
		"content type":     func(r *http.Request) { r.Header.Del("Content-Type") },
		"body hash":        func(r *http.Request) { r.Header.Set("X-Content-Sha256", "unrelated") },
		"if match":         func(r *http.Request) { r.Header.Set("If-Match", "*") },
		"duplicate match":  func(r *http.Request) { r.Header.Add("If-Match", "*") },
		"case alias":       func(r *http.Request) { r.Header["if-match"] = []string{"*"} },
		"retry header":     func(r *http.Request) { r.Header.Set("Idempotency-Key", "retry") },
		"request encoding": func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
		"mutation during body close": func(r *http.Request) {
			r.Body = oracleBodyWithClose{r.Body, func() { r.Header.Set("If-Match", "*") }}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			client, err := NewClient(func(req *http.Request) error {
				req.Header.Set("Authorization", "synthetic-signature")
				mutate(req)
				return nil
			}, roundTrip(func(*http.Request) (*http.Response, error) {
				t.Fatal("mutated request reached transport")
				return nil, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ScheduleOracleOldVersionDeletionWithFence(context.Background(), authorized, "ap-osaka-1", oracleFixtureSecret, "fixture-tag", 7, time.Now().Add(48*time.Hour), func(context.Context) error {
				t.Fatal("mutated request reached fence")
				return nil
			})
			if err == nil || errors.Is(err, ErrOracleEffectUnknown) {
				t.Fatal("expected pre-dispatch signer rejection")
			}
		})
	}
}

func TestOracleFinalFenceAndUnknownEffect(t *testing.T) {
	var sends int
	client := clientFixture(t, func(req *http.Request) (*http.Response, error) {
		sends++
		if req.Method != http.MethodPost || req.URL.Path != "/20180608/secrets/"+oracleFixtureSecret+"/version/7/actions/scheduleDeletion" || req.Header.Get("If-Match") != "fixture-tag" {
			t.Fatal("wrong deletion scheduling request")
		}
		return nil, errors.New("fixture lost reply")
	})
	denied := errors.New("fence lost")
	deleteAt := time.Now().Add(48 * time.Hour)
	_, err := client.ScheduleOracleOldVersionDeletionWithFence(context.Background(), authorized, "ap-osaka-1", oracleFixtureSecret, "fixture-tag", 7, deleteAt, func(context.Context) error { return denied })
	if !errors.Is(err, denied) || errors.Is(err, ErrOracleEffectUnknown) || sends != 0 {
		t.Fatalf("unexpected fence result %v after %d sends", err, sends)
	}
	_, err = client.ScheduleOracleOldVersionDeletionWithFence(context.Background(), authorized, "ap-osaka-1", oracleFixtureSecret, "fixture-tag", 7, deleteAt, func(context.Context) error { return nil })
	if !errors.Is(err, ErrOracleEffectUnknown) || sends != 1 || strings.Contains(err.Error(), "fixture lost reply") {
		t.Fatalf("unexpected dispatch result %v after %d sends", err, sends)
	}
}
