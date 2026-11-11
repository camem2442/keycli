// Command kc-effectclient submits or reads one signed KeyCLI gateway operation.
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/camem2442/keycli/internal/effectgateway"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("kc-effectclient", flag.ContinueOnError)
	flags.SetOutput(stderr)
	mode := flags.String("mode", "operational", "operational or synthetic")
	config := flags.String("config", "", "protected operational gateway origin configuration")
	keyFile := flags.String("key-file", "", "protected operational session key file")
	operation := flags.String("operation", "submit", "submit, status or reconcile")
	submitURL := flags.String("url", "", "synthetic gateway submit URL")
	resolve := flags.String("resolve", "", "synthetic host:port:ip override")
	dryRun := flags.Bool("dry-run", false, "print a synthetic envelope without sending")
	var req effectgateway.Request
	flags.StringVar(&req.GoalID, "goal", "", "goal id")
	flags.StringVar(&req.MutationID, "mutation", "", "mutation id")
	flags.StringVar(&req.Provider, "provider", "cloudflare", "provider name")
	flags.StringVar(&req.Action, "action", "issue", "provider action")
	flags.StringVar(&req.TargetDigest, "target", "", "target digest")
	flags.StringVar(&req.PlanDigest, "plan", "", "plan digest")
	flags.StringVar(&req.GrantDigest, "grant", "", "grant digest")
	flags.StringVar(&req.Owner, "owner", "", "claim owner")
	flags.StringVar(&req.Session, "session", "", "claim session")
	flags.Int64Var(&req.Generation, "generation", 0, "claim generation")
	ttl := flags.Duration("ttl", time.Minute, "request expiry")
	timeout := flags.Duration("timeout", 20*time.Second, "http timeout")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 64
	}
	if flags.NArg() != 0 || req.GoalID == "" || req.MutationID == "" || req.Owner == "" ||
		req.Session == "" || req.Generation < 1 {
		fmt.Fprintln(stderr, "goal, mutation, owner, session and positive generation are required")
		return 64
	}
	if *operation == "submit" && (req.TargetDigest == "" || req.PlanDigest == "" || req.GrantDigest == "") {
		fmt.Fprintln(stderr, "target, plan and grant are required for submit")
		return 64
	}
	req.ExpiresAt = time.Now().Add(*ttl).UTC()
	switch *mode {
	case "operational":
		if *submitURL != "" || *resolve != "" || *dryRun || *config == "" || *keyFile == "" {
			fmt.Fprintln(stderr, "operational mode requires config and key-file and rejects url, resolve and dry-run")
			return 64
		}
		if *operation != "submit" && *operation != effectgateway.ReadStatus && *operation != effectgateway.ReadReconcile {
			fmt.Fprintln(stderr, "operation must be submit, status or reconcile")
			return 64
		}
		if *ttl <= 0 || *ttl > 10*time.Minute || *timeout <= 0 {
			fmt.Fprintln(stderr, "operational mode requires a positive timeout and a TTL of up to ten minutes")
			return 64
		}
		origin, err := loadOrigin(*config)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 64
		}
		key, err := effectgateway.ReadSessionKeyFile(*keyFile)
		if err != nil {
			fmt.Fprintln(stderr, "invalid operational session key file")
			return 64
		}
		defer clear(key)
		return runOperational(operationalHTTPClient(*timeout), origin, *operation, req, key, *ttl, stdout, stderr)
	case "synthetic":
		if *submitURL == "" || *config != "" || *keyFile != "" || *operation != "submit" {
			fmt.Fprintln(stderr, "synthetic mode requires url and supports only submit using KC_EFFECT_KEY")
			return 64
		}
		return runSynthetic(*submitURL, *resolve, *dryRun, *timeout, req, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "mode must be operational or synthetic")
		return 64
	}
}

func loadOrigin(path string) (url.URL, error) {
	invalid := errors.New("invalid protected gateway origin configuration")
	raw, err := effectgateway.ReadPrivateFile(path, 8<<10)
	if err != nil {
		return url.URL{}, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return url.URL{}, invalid
	}
	var origin string
	var version int
	fields := map[string]any{"origin": &origin, "v": &version}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return url.URL{}, invalid
		}
		name, ok := token.(string)
		if !ok || fields[name] == nil || decoder.Decode(fields[name]) != nil {
			return url.URL{}, invalid
		}
		delete(fields, name)
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return url.URL{}, invalid
	}
	if _, err := decoder.Token(); err != io.EOF || len(fields) != 0 || version != 1 {
		return url.URL{}, invalid
	}
	endpoint, err := url.Parse(origin)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
		endpoint.Opaque != "" || endpoint.User != nil || endpoint.RawPath != "" ||
		(endpoint.Path != "" && endpoint.Path != "/") || endpoint.ForceQuery ||
		endpoint.RawQuery != "" || strings.ContainsAny(origin, "#%") || strings.HasSuffix(endpoint.Host, ":") {
		return url.URL{}, invalid
	}
	if port := endpoint.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return url.URL{}, invalid
		}
	}
	return url.URL{Scheme: endpoint.Scheme, Host: endpoint.Host}, nil
}

func operationalHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:       (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
			ForceAttemptHTTP2: true,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func runOperational(client *http.Client, origin url.URL, operation string, req effectgateway.Request, key []byte, ttl time.Duration, stdout, stderr io.Writer) int {
	ctx := context.Background()
	var connected atomic.Bool
	var envelope any
	if operation == "submit" {
		envelope = effectgateway.Sign(req, key)
		ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
			GotConn: func(httptrace.GotConnInfo) { connected.Store(true) },
		})
	} else {
		envelope = signRead(operation, req, key, req.ExpiresAt)
	}
	status, out, err := sendOperational(ctx, client, origin, operation, req.MutationID, envelope)
	if err != nil && operation == "submit" && connected.Load() {
		// Submission may have reached the provider. Only read the original mutation.
		status, out, err = sendOperational(context.Background(), client, origin, effectgateway.ReadStatus, req.MutationID,
			signRead(effectgateway.ReadStatus, req, key, time.Now().Add(ttl).UTC()))
		if err != nil || status != http.StatusOK {
			fmt.Fprintln(stdout, `{"status":"SUBMIT_UNCONFIRMED"}`)
			fmt.Fprintln(stderr, "submit response unavailable and original mutation status could not be confirmed")
			return 2
		}
	}
	if err != nil {
		fmt.Fprintln(stdout, `{"status":"TRANSPORT_ERROR"}`)
		fmt.Fprintln(stderr, "gateway response unavailable")
		return 2
	}
	if status != http.StatusOK {
		fmt.Fprintf(stdout, "http=%d\n", status)
		return 3
	}
	fmt.Fprintf(stdout, "http=%d %s\n", status, out)
	return 0
}

func signRead(operation string, req effectgateway.Request, key []byte, expires time.Time) effectgateway.ReadEnvelope {
	return effectgateway.SignRead(effectgateway.ReadRequest{
		GoalID: req.GoalID, MutationID: req.MutationID, Owner: req.Owner, Session: req.Session,
		Generation: req.Generation, Action: operation, ExpiresAt: expires,
	}, key)
}

func sendOperational(ctx context.Context, client *http.Client, origin url.URL, operation, mutation string, envelope any) (int, []byte, error) {
	origin.Path = "/" + operation
	resp, err := post(ctx, client, origin.String(), envelope, false)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil, nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, effectgateway.MaxEnvelopeBytes+1))
	if err != nil || len(raw) > effectgateway.MaxEnvelopeBytes {
		return 0, nil, errors.New("gateway receipt unavailable")
	}
	var receipt effectgateway.Receipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.MutationID != mutation || receipt.Status == "" {
		return 0, nil, errors.New("invalid gateway receipt")
	}
	out, err := json.Marshal(receipt)
	return resp.StatusCode, out, err
}

func post(ctx context.Context, client *http.Client, endpoint string, envelope any, allowReplay bool) (*http.Response, error) {
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if !allowReplay {
		req.GetBody = nil
	}
	req.Header.Set("Content-Type", "application/json")
	return client.Do(req)
}

func runSynthetic(endpoint, resolve string, dryRun bool, timeout time.Duration, req effectgateway.Request, stdout, stderr io.Writer) int {
	key, err := effectgateway.ParseHexKey(strings.TrimSpace(os.Getenv("KC_EFFECT_KEY")))
	if err != nil {
		fmt.Fprintln(stderr, "KC_EFFECT_KEY:", err)
		return 64
	}
	defer clear(key)
	envelope := effectgateway.Sign(req, key)
	if dryRun {
		if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
			fmt.Fprintln(stderr, "encode:", err)
			return 1
		}
		return 0
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if resolve != "" {
		parts := strings.SplitN(resolve, ":", 3)
		if len(parts) != 3 {
			fmt.Fprintln(stderr, "-resolve expects host:port:ip")
			return 64
		}
		ipPort := net.JoinHostPort(parts[2], parts[1])
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, ipPort)
		}
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	resp, err := post(context.Background(), client, endpoint, envelope, true)
	if err != nil {
		fmt.Fprintln(stdout, `{"status":"TRANSPORT_ERROR"}`)
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	fmt.Fprintf(stdout, "http=%d %s\n", resp.StatusCode, strings.TrimSpace(string(out)))
	if resp.StatusCode != http.StatusOK {
		return 3
	}
	return 0
}
