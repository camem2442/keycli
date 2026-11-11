package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
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
	"testing"
	"time"

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
	out, err = execute(nil, "-mode", "synthetic", "-url", "http://127.0.0.1:1/submit", "-dry-run")
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
			_ = json.NewEncoder(w).Encode(effectgateway.Receipt{
				MutationID: "mutation", Status: effectgateway.StatusApplied,
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
		out, err = execute(nil, "-config", config, "-key-file", keyFile)
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
				"-config", config, "-key-file", keyFile)
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
		origin, err := loadOrigin(path)
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
			expected := signRead("status", req, key, env.Request.ExpiresAt)
			if err != nil || env != expected || !env.Request.ExpiresAt.After(req.ExpiresAt) {
				t.Error("status must freshly sign the original mutation and claim identity")
			}
			_ = json.NewEncoder(w).Encode(effectgateway.Receipt{
				MutationID: req.MutationID, Status: effectgateway.StatusUnknown,
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
	if code := runOperational(operationalHTTPClient(time.Second), origin, "status", req, key, time.Minute, io.Discard, io.Discard); code != 2 {
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
	client := operationalHTTPClient(timeout)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	client.Transport.(*http.Transport).TLSClientConfig.RootCAs = pool
	t.Cleanup(client.CloseIdleConnections)
	return *origin, client
}
