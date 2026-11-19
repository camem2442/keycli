// Command kc-effectclient submits or reads one signed KeyCLI gateway operation.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/effectclient"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("kc-effectclient", flag.ContinueOnError)
	flags.SetOutput(stderr)
	showVersion := flags.Bool("version", false, "print the build version and source commit")
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
	providerOperation := flags.String("provider-operation", "", "provider operation that selects the admission action")
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
	actionSet := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "action" {
			actionSet = true
		}
	})
	if *showVersion && len(args) == 1 {
		fmt.Fprintln(stdout, version.String())
		return 0
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
		// The admission class of provider-operation is fixed by the gateway, and
		// the operational gateway refuses a request that carries the operation
		// field. Classify it here so a fixed action is signed and a mislabelled
		// one is refused before the key is read or anything leaves the process.
		if *providerOperation == "" {
			fmt.Fprintln(stderr, "operational mode requires -provider-operation")
			return 64
		}
		action, err := resolveAction(req.Provider, *providerOperation, req.Action, actionSet)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 64
		}
		req.Action = action
		origin, err := effectclient.LoadOrigin(*config)
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
		return runOperational(effectclient.NewHTTPClient(*timeout), origin, *operation, req, key, *ttl, stdout, stderr)
	case "synthetic":
		if *submitURL == "" || *config != "" || *keyFile != "" || *operation != "submit" {
			fmt.Fprintln(stderr, "synthetic mode requires url and supports only submit using KC_EFFECT_KEY")
			return 64
		}
		if *providerOperation == "" {
			fmt.Fprintln(stderr, "synthetic submit requires -provider-operation")
			return 64
		}
		req.Operation = *providerOperation
		action, err := resolveAction(req.Provider, req.Operation, req.Action, actionSet)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 64
		}
		req.Action = action
		return runSynthetic(*submitURL, *resolve, *dryRun, *timeout, req, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "mode must be operational or synthetic")
		return 64
	}
}

// resolveAction returns the admission action implied by provider/operation.
// The gateway fixes that class, and sending a differently labelled -action is
// refused there as ADMISSION_REJECTED only after the trusted host has already
// been consulted, which hides the cause. Deriving or checking it here keeps
// the client from signing a request it knows is mislabelled. A missing
// -provider-operation is left to the caller so each mode can name its own flag.
func resolveAction(provider, operation, action string, actionSet bool) (string, error) {
	expected, _, err := hostbridge.ClassifyOperation(provider, operation)
	if err != nil {
		return "", fmt.Errorf("provider-operation %q is not admitted for provider %q", operation, provider)
	}
	if actionSet && action != expected {
		return "", fmt.Errorf("-action %q does not match %s/%s, which is %q; omit -action or send %q",
			action, provider, operation, expected, expected)
	}
	return expected, nil
}

func runOperational(client *http.Client, origin url.URL, operation string, req effectgateway.Request, key []byte, ttl time.Duration, stdout, stderr io.Writer) int {
	result, err := effectclient.Execute(context.Background(), client, origin, operation, req, key, ttl)
	if errors.Is(err, effectclient.ErrSubmitUnconfirmed) {
		fmt.Fprintln(stdout, `{"status":"SUBMIT_UNCONFIRMED"}`)
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stdout, `{"status":"TRANSPORT_ERROR"}`)
		fmt.Fprintln(stderr, "gateway response unavailable")
		return 2
	}
	if result.HTTPStatus != http.StatusOK {
		fmt.Fprintf(stdout, "http=%d\n", result.HTTPStatus)
		return 3
	}
	out, _ := json.Marshal(result.Receipt)
	fmt.Fprintf(stdout, "http=%d %s\n", result.HTTPStatus, out)
	return 0
}

func post(ctx context.Context, client *http.Client, endpoint string, envelope effectgateway.Envelope) (*http.Response, error) {
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
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
	resp, err := post(context.Background(), client, endpoint, envelope)
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
