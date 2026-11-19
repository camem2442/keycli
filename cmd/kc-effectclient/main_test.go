package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectclient"
	"github.com/camem2442/keycli/internal/effectgateway"
)

func TestInstalledEffectClientModes(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "kc-effectclient")
	build := []string{"build", "-o", binary}
	if runtime.GOOS == "darwin" && strings.HasPrefix(runtime.Version(), "go1.22") {
		// Go 1.22's internal linker omits the LC_UUID required by newer macOS.
		build = append(build, "-ldflags=-linkmode=external")
	}
	if out, err := exec.Command("go", append(build, ".")...).CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	args := []string{"-goal", "goal", "-mutation", "mutation", "-owner", "owner", "-session", "session",
		"-generation", "1", "-target", strings.Repeat("a", 64), "-plan", strings.Repeat("b", 64),
		"-grant", strings.Repeat("c", 64)}
	key := strings.Repeat("f", 64)
	execute := func(env []string, extra ...string) ([]byte, error) {
		cmd := exec.Command(binary, append(args, extra...)...)
		cmd.Env = append(append(os.Environ(), "KC_EFFECT_KEY="+key), env...)
		return cmd.CombinedOutput()
	}

	out, err := execute(nil)
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 64 || !bytes.Contains(out, []byte("requires config and key-file")) {
		t.Fatalf("missing operational configuration: %v\n%s", err, out)
	}
	out, err = execute(nil, "-url", "http://127.0.0.1:1/submit", "-dry-run")
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 64 || bytes.Contains(out, []byte("signature")) {
		t.Fatalf("operational overrides: %v\n%s", err, out)
	}
	out, err = execute(nil, "-mode", "synthetic", "-url", "http://127.0.0.1:1/submit", "-dry-run",
		"-provider", "synthetic", "-provider-operation", "record-local-test")
	var env effectgateway.Envelope
	if err != nil || json.Unmarshal(out, &env) != nil || env.Request.MutationID != "mutation" || env.Signature == "" || bytes.Contains(out, []byte(key)) {
		t.Fatalf("synthetic dry-run: %v", err)
	}

	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var signed effectgateway.Envelope
			keyBytes, _ := effectgateway.ParseHexKey(key)
			if r.URL.Path != "/submit" || json.NewDecoder(r.Body).Decode(&signed) != nil ||
				signed.Signature != effectgateway.Sign(signed.Request, keyBytes).Signature {
				t.Error("installed client must sign with the operational file key")
			}
			// The operational gateway refuses a request that carries the
			// operation field, and the signature check above cannot catch a
			// re-introduced one because it would be signed too.
			if signed.Request.Operation != "" {
				t.Errorf("operational envelope carries operation %q", signed.Request.Operation)
			}
			_ = json.NewEncoder(w).Encode(effectgateway.Receipt{
				MutationID: "mutation", Status: effectgateway.StatusApplied,
				OperationState: string(journal.Acknowledged), ProviderCalls: 1, ProofDigest: strings.Repeat("a", 64),
			})
		}))
		defer server.Close()
		dir, err := filepath.EvalSymlinks(filepath.Dir(binary))
		if err != nil {
			t.Fatal(err)
		}
		config := filepath.Join(dir, "client.json")
		keyFile := filepath.Join(dir, "key")
		if err := os.WriteFile(config, []byte(`{"v":1,"origin":"`+server.URL+`"}`), 0600); err != nil {
			t.Fatal(err)
		}
		out, err = execute(nil, "-config", config, "-key-file", keyFile, "-provider-operation", "create-account-token")
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 64 || !bytes.Contains(out, []byte("invalid operational session key file")) {
			t.Fatalf("missing session key file with KC_EFFECT_KEY set: %v\n%s", err, out)
		}
		if err := os.WriteFile(keyFile, []byte(key+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "linux" {
			cert := filepath.Join(dir, "gateway.pem")
			if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			out, err = execute([]string{"SSL_CERT_FILE=" + cert, "KC_EFFECT_KEY=" + strings.Repeat("a", 64)},
				"-config", config, "-key-file", keyFile, "-provider-operation", "create-account-token")
			if err != nil || !bytes.Contains(out, []byte(effectgateway.StatusApplied)) || bytes.Contains(out, []byte(key)) {
				t.Fatalf("operational HTTPS: %v\n%s", err, out)
			}
		}
	}
}

func TestLoadOrigin(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("protected files require Linux or macOS")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "client.json")
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"v":1,"origin":"https://gateway.example:443"}`, true},
		{`{"origin":"https://[::1]:443/","v":1}`, true},
		{`{"v":1,"origin":"http://gateway.example"}`, false},
		{`{"v":1,"origin":"https://gateway.example/submit"}`, false},
		{`{"v":1,"origin":"https://gateway.example?"}`, false},
		{`{"v":1,"origin":"https://gateway.example#"}`, false},
		{`{"v":1,"origin":"https://gateway.example:0"}`, false},
		{`{"v":1,"origin":"https://user@gateway.example"}`, false},
		{`{"v":1,"origin":"https://gateway.example","origin":"https://other.example"}`, false},
		{`{"v":1,"origin":"https://gateway.example"} {}`, false},
		{`{"v":1,"origin":"https://gateway.example","proxy":"https://other.example"}`, false},
	} {
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		origin, err := effectclient.LoadOrigin(path)
		if (err == nil) != tc.ok || (tc.ok && origin.Path != "") {
			t.Errorf("loadOrigin(%s) = %s, %v", tc.body, origin.String(), err)
		}
	}
}

func TestOperationalSubmitTimeoutReadsOriginalMutation(t *testing.T) {
	req := effectgateway.Request{GoalID: "goal", MutationID: "mutation", Owner: "owner",
		Session: "session", Generation: 2, ExpiresAt: time.Now().Add(time.Minute).UTC()}
	key := make([]byte, 32)
	var mu sync.Mutex
	var paths []string
	receivedStatus := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/submit":
			var env effectgateway.Envelope
			if err := json.NewDecoder(r.Body).Decode(&env); err != nil || env.Signature != effectgateway.Sign(req, key).Signature {
				t.Error("unexpected signed submit")
			}
			select {
			case <-receivedStatus:
			case <-time.After(3 * time.Second):
			}
		case "/status":
			defer close(receivedStatus)
			var env effectgateway.ReadEnvelope
			err := json.NewDecoder(r.Body).Decode(&env)
			expected := effectgateway.SignRead(effectgateway.ReadRequest{
				GoalID: req.GoalID, MutationID: req.MutationID, Owner: req.Owner, Session: req.Session,
				Generation: req.Generation, Action: effectgateway.ReadStatus, ExpiresAt: env.Request.ExpiresAt,
			}, key)
			if err != nil || env != expected || !env.Request.ExpiresAt.After(req.ExpiresAt) {
				t.Error("status must freshly sign the original mutation and claim identity")
			}
			_ = json.NewEncoder(w).Encode(effectgateway.Receipt{
				MutationID: req.MutationID, Status: effectgateway.StatusUnknown,
				OperationState: string(journal.UnknownEffect),
			})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	origin, client := trustedTestGateway(t, server, time.Second)

	var out, stderr bytes.Buffer
	code := runOperational(client, origin, "submit", req, key, time.Minute, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), effectgateway.StatusUnknown) || stderr.Len() > 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out.String(), stderr.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(paths, ",") != "/submit,/status" {
		t.Fatalf("unexpected requests %v", paths)
	}
}

func TestOperationalSubmitUnconfirmed(t *testing.T) {
	req := effectgateway.Request{MutationID: "mutation", ExpiresAt: time.Now().Add(time.Minute)}
	key := make([]byte, 32)
	var mu sync.Mutex
	var paths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/submit":
			_, _ = io.WriteString(w, `{"mutation_id":"different","status":"APPLIED"}`)
		case "/status":
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":"STALE_GENERATION"}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	origin, client := trustedTestGateway(t, server, time.Second)

	result, err := effectclient.Execute(context.Background(), client, origin, effectclient.Submit, req, key, time.Minute)
	if !errors.Is(err, effectclient.ErrSubmitUnconfirmed) || !result.StatusFallback ||
		result.HTTPStatus != http.StatusConflict || result.ErrorCode != "STALE_GENERATION" ||
		result.Receipt != (effectgateway.Receipt{}) {
		t.Fatalf("expected unconfirmed submit with status fallback, got %+v, %v", result, err)
	}
	result, err = effectclient.Execute(context.Background(), effectclient.NewHTTPClient(time.Second), origin,
		effectclient.Submit, req, key, time.Minute)
	if !errors.Is(err, effectclient.ErrTransport) || result.StatusFallback {
		t.Fatalf("expected TLS failure without status fallback, got %+v, %v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(paths, ",") != "/submit,/status" {
		t.Fatalf("unexpected requests %v", paths)
	}
}

func TestOperationalTLSAndRedirect(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		http.Redirect(w, r, "/alternate", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	origin, trusted := trustedTestGateway(t, server, time.Second)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	if trusted.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("operational transport must bypass proxy configuration")
	}
	req := effectgateway.Request{MutationID: "mutation"}
	key := make([]byte, 32)
	if code := runOperational(effectclient.NewHTTPClient(time.Second), origin, "status", req, key, time.Minute, io.Discard, io.Discard); code != 2 {
		t.Fatalf("untrusted TLS exit %d", code)
	}
	if code := runOperational(trusted, origin, "status", req, key, time.Minute, io.Discard, io.Discard); code != 3 {
		t.Fatalf("redirect exit %d", code)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(paths, ",") != "/status" {
		t.Fatalf("unexpected requests %v", paths)
	}
}

func trustedTestGateway(t *testing.T, server *httptest.Server, timeout time.Duration) (url.URL, *http.Client) {
	t.Helper()
	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := effectclient.NewHTTPClient(timeout)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.Transport.(*http.Transport).TLSClientConfig.RootCAs = pool
	t.Cleanup(client.CloseIdleConnections)
	return *origin, client
}

func TestOperationalSubmitIntermediaryFailureReadsStatusOnce(t *testing.T) {
	for _, tc := range []struct {
		name           string
		status         int
		body           string
		fallback       bool
		statusRejected bool
		statusBody     string
	}{
		{name: "bad gateway", status: http.StatusBadGateway, body: "upstream unavailable", fallback: true},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, body: "upstream timed out", fallback: true},
		{name: "intermediary unavailable", status: http.StatusServiceUnavailable, body: "no upstream", fallback: true},
		{name: "unconfirmed status", status: http.StatusBadGateway, fallback: true, statusRejected: true},
		{name: "incomplete submit receipt", status: http.StatusOK, fallback: true,
			body: `{"mutation_id":"mutation","status":"APPLIED","provider_calls":1}`},
		{name: "incomplete status receipt", status: http.StatusBadGateway, fallback: true,
			statusBody: `{"mutation_id":"mutation","status":"UNKNOWN_EFFECT","operation_state":"UNKNOWN_EFFECT"}`},
		{name: "gateway denial", status: http.StatusForbidden, body: `{"error":"ADMISSION_REJECTED"}`},
		{name: "gateway unavailable", status: http.StatusServiceUnavailable, body: `{"error":"AUTHORITY_UNAVAILABLE"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := effectgateway.Request{GoalID: "goal", MutationID: "mutation", Owner: "owner",
				Session: "session", Generation: 2, ExpiresAt: time.Now().Add(10 * time.Second).UTC()}
			key := make([]byte, 32)
			var mu sync.Mutex
			var paths []string
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				switch r.URL.Path {
				case "/submit":
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
				case "/status":
					var env effectgateway.ReadEnvelope
					err := json.NewDecoder(r.Body).Decode(&env)
					expected := effectgateway.SignRead(effectgateway.ReadRequest{
						GoalID: req.GoalID, MutationID: req.MutationID, Owner: req.Owner, Session: req.Session,
						Generation: req.Generation, Action: effectgateway.ReadStatus, ExpiresAt: env.Request.ExpiresAt,
					}, key)
					if r.Method != http.MethodPost || err != nil || env != expected || !env.Request.ExpiresAt.After(req.ExpiresAt) {
						t.Error("status must freshly sign the original mutation and claim identity")
					}
					if tc.statusRejected {
						w.WriteHeader(http.StatusGatewayTimeout)
						return
					}
					if tc.statusBody != "" {
						_, _ = io.WriteString(w, tc.statusBody)
						return
					}
					_ = json.NewEncoder(w).Encode(effectgateway.Receipt{
						MutationID: req.MutationID, Status: effectgateway.StatusUnknown,
						OperationState: string(journal.UnknownEffect),
					})
				default:
					t.Errorf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()
			origin, client := trustedTestGateway(t, server, 3*time.Second)
			result, err := effectclient.Execute(context.Background(), client, origin, effectclient.Submit, req, key, time.Minute)
			if result.StatusFallback != tc.fallback {
				t.Fatalf("unexpected fallback: %+v, %v", result, err)
			}
			switch {
			case tc.statusRejected || tc.statusBody != "":
				if !errors.Is(err, effectclient.ErrSubmitUnconfirmed) {
					t.Fatalf("want unconfirmed, got %+v, %v", result, err)
				}
			case tc.fallback:
				if err != nil || result.Receipt.MutationID != req.MutationID || result.Receipt.Status != effectgateway.StatusUnknown {
					t.Fatalf("want original mutation status, got %+v, %v", result, err)
				}
			default:
				if err != nil || result.HTTPStatus != tc.status || result.ErrorCode == "" {
					t.Fatalf("want original gateway refusal, got %+v, %v", result, err)
				}
			}
			expectedPaths := "/submit"
			if tc.fallback {
				expectedPaths += ",/status"
			}
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(paths, ",") != expectedPaths {
				t.Fatalf("unexpected requests %v", paths)
			}
		})
	}
}

func TestSyntheticActionFollowsOperationClass(t *testing.T) {
	t.Setenv("KC_EFFECT_KEY", strings.Repeat("f", 64))
	base := []string{"-mode", "synthetic", "-url", "http://127.0.0.1:1/submit", "-dry-run",
		"-goal", "goal", "-mutation", "mutation", "-owner", "owner", "-session", "session",
		"-generation", "1", "-target", strings.Repeat("a", 64), "-plan", strings.Repeat("b", 64),
		"-grant", strings.Repeat("c", 64), "-provider", "synthetic",
		"-provider-operation", "record-local-test"}

	t.Run("default action follows the admitted class", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if code := run(base, &out, &errOut); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, errOut.String())
		}
		var env effectgateway.Envelope
		if err := json.Unmarshal(out.Bytes(), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// Pinned literally: comparing with ClassifyOperation itself would pass
		// even if the table stopped mapping this operation to "stage".
		if env.Request.Action != "stage" {
			t.Fatalf("action %q, want %q", env.Request.Action, "stage")
		}
	})

	t.Run("explicit mismatch is refused before sending", func(t *testing.T) {
		args := append(append([]string{}, base...), "-action", "issue")
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 64 {
			t.Fatalf("exit %d, want 64; stderr %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "does not match") || out.Len() != 0 {
			t.Fatalf("stdout %q, stderr %q", out.String(), errOut.String())
		}
	})

	t.Run("explicit match is unchanged", func(t *testing.T) {
		args := append(append([]string{}, base...), "-action", "stage")
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 0 {
			t.Fatalf("exit %d, stderr %q", code, errOut.String())
		}
		var env effectgateway.Envelope
		if err := json.Unmarshal(out.Bytes(), &env); err != nil || env.Request.Action != "stage" {
			t.Fatalf("action %q (%v)", env.Request.Action, err)
		}
	})

	t.Run("missing provider-operation is refused", func(t *testing.T) {
		args := append(append([]string{}, base...), "-provider-operation", "")
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 64 || !strings.Contains(errOut.String(), "requires -provider-operation") {
			t.Fatalf("exit %d, stderr %q", code, errOut.String())
		}
	})

	t.Run("unknown operation is refused", func(t *testing.T) {
		args := append(append([]string{}, base...), "-provider-operation", "not-admitted")
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 64 || !strings.Contains(errOut.String(), "not admitted") {
			t.Fatalf("exit %d, stderr %q", code, errOut.String())
		}
	})
}

// TestSyntheticMismatchNeverReachesTheGateway proves the refusal happens before
// any request is built or sent, not merely before the response is printed.
func TestSyntheticMismatchNeverReachesTheGateway(t *testing.T) {
	t.Setenv("KC_EFFECT_KEY", strings.Repeat("f", 64))
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	args := []string{"-mode", "synthetic", "-url", server.URL + "/submit",
		"-goal", "goal", "-mutation", "mutation", "-owner", "owner", "-session", "session",
		"-generation", "1", "-target", strings.Repeat("a", 64), "-plan", strings.Repeat("b", 64),
		"-grant", strings.Repeat("c", 64), "-provider", "synthetic",
		"-provider-operation", "record-local-test", "-action", "issue"}
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != 64 {
		t.Fatalf("exit %d, want 64; stderr %q", code, errOut.String())
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("gateway received %d request(s) for a mislabelled action", got)
	}
	if !strings.Contains(errOut.String(), "does not match") {
		t.Fatalf("stderr %q", errOut.String())
	}
}

// operationalArgs is a complete, otherwise valid operational invocation. The
// gateway origin and key file are supplied by each case.
func operationalArgs() []string {
	return []string{"-mode", "operational", "-goal", "goal", "-mutation", "mutation",
		"-owner", "owner", "-session", "session", "-generation", "1",
		"-target", strings.Repeat("a", 64), "-plan", strings.Repeat("b", 64),
		"-grant", strings.Repeat("c", 64)}
}

// TestOperationalActionFollowsProviderOperation exercises the pure decision.
// The operational signing path needs a trusted HTTPS origin, which run() takes
// only from the system trust store, so the classification lives in its own
// helper that can be proven without a gateway.
func TestOperationalActionFollowsProviderOperation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		provider   string
		operation  string
		action     string
		actionSet  bool
		wantAction string
		wantErr    string
	}{
		// The pinned flag default must not survive an omitted -action.
		{name: "omitted action derives the issued class", provider: "cloudflare",
			operation: "create-account-token", action: "issue", wantAction: "issue"},
		{name: "omitted action replaces a stale default", provider: "github",
			operation: "stage-repository-generation-secret", action: "issue", wantAction: "stage"},
		{name: "explicit match is unchanged", provider: "cloudflare",
			operation: "delete-exact-account-token", action: "revoke", actionSet: true, wantAction: "revoke"},
		{name: "explicit mismatch is refused", provider: "cloudflare",
			operation: "create-account-token", action: "stage", actionSet: true, wantErr: `"issue"`},
		{name: "unknown operation is refused", provider: "cloudflare",
			operation: "not-admitted", wantErr: "not admitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action, err := resolveAction(tc.provider, tc.operation, tc.action, tc.actionSet)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("action %q, err %v; want error containing %q", action, err, tc.wantErr)
				}
				if action != "" {
					t.Fatalf("refused classification returned action %q", action)
				}
				return
			}
			if err != nil || action != tc.wantAction {
				t.Fatalf("action %q, err %v; want %q", action, err, tc.wantAction)
			}
		})
	}
}

// TestOperationalRefusesBeforeReadingKey proves the usage checks run before any
// file is opened. The sentinel key file holds bytes that are not a session key,
// and the sentinel config names an origin that is never loaded, so a refusal
// that came after either read would surface their errors instead.
func TestOperationalRefusesBeforeReadingKey(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("protected files require Linux or macOS")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(dir, "session.key")
	if err := os.WriteFile(keyFile, []byte("not-a-session-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "client.json")
	if err := os.WriteFile(config, []byte(`{"v":1,"origin":"https://127.0.0.1:1"}`), 0600); err != nil {
		t.Fatal(err)
	}

	t.Run("missing provider-operation on submit", func(t *testing.T) {
		args := append(operationalArgs(), "-config", filepath.Join(dir, "absent.json"),
			"-key-file", filepath.Join(dir, "absent.key"))
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 64 {
			t.Fatalf("exit %d, want 64; stderr %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "requires -provider-operation") {
			t.Fatalf("stderr %q", errOut.String())
		}
		if out.Len() != 0 {
			t.Fatalf("stdout %q", out.String())
		}
		// LoadOrigin runs first after the usage checks, so its error would be
		// the observable proof that anything was read.
		if strings.Contains(errOut.String(), "origin configuration") {
			t.Fatalf("validated after reading the config: %q", errOut.String())
		}
	})

	t.Run("status and reconcile need no provider-operation", func(t *testing.T) {
		// A read envelope signs Action=operation and never sends req.Action, so
		// demanding a provider operation here would break the documented
		// recovery after an ambiguous submit without gaining an admission check.
		for _, operation := range []string{"status", "reconcile"} {
			args := append(operationalArgs(), "-operation", operation,
				"-config", filepath.Join(dir, "absent.json"),
				"-key-file", filepath.Join(dir, "absent.key"))
			var out, errOut bytes.Buffer
			if code := run(args, &out, &errOut); code != 64 {
				t.Fatalf("%s: exit %d, want 64; stderr %q", operation, code, errOut.String())
			}
			if strings.Contains(errOut.String(), "requires -provider-operation") {
				t.Fatalf("%s: read path demanded a provider operation: %q", operation, errOut.String())
			}
			if !strings.Contains(errOut.String(), "origin configuration") {
				t.Fatalf("%s: did not reach LoadOrigin: %q", operation, errOut.String())
			}
		}
	})

	t.Run("mismatched action", func(t *testing.T) {
		args := append(operationalArgs(), "-config", config, "-key-file", keyFile,
			"-provider", "cloudflare", "-provider-operation", "create-account-token", "-action", "stage")
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != 64 {
			t.Fatalf("exit %d, want 64; stderr %q", code, errOut.String())
		}
		if !strings.Contains(errOut.String(), "does not match") ||
			!strings.Contains(errOut.String(), `"issue"`) {
			t.Fatalf("stderr %q", errOut.String())
		}
		if strings.Contains(errOut.String(), "session key file") {
			t.Fatalf("signed path reached the key file: %q", errOut.String())
		}
		if out.Len() != 0 {
			t.Fatalf("stdout %q", out.String())
		}
	})
}
