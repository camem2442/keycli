// Command kc-effectclient submits one signed intent to a KeyCLI effect gateway.
//
// The session key is read from the KC_EFFECT_KEY environment variable so it never
// appears in argv or in the shell history.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/effectgateway"
)

func main() {
	url := flag.String("url", "", "gateway submit URL, e.g. https://host.tailnet.ts.net:8457/submit")
	goal := flag.String("goal", "", "goal id")
	mutation := flag.String("mutation", "", "mutation id")
	provider := flag.String("provider", "cloudflare", "provider name")
	operation := flag.String("operation", "create-account-token", "exact provider operation id (synthetic default)")
	action := flag.String("action", "issue", "provider action class")
	target := flag.String("target", "", "target digest")
	plan := flag.String("plan", "", "plan digest")
	grant := flag.String("grant", "", "grant digest")
	owner := flag.String("owner", "", "claim owner")
	session := flag.String("session", "", "claim session")
	generation := flag.Int64("generation", 0, "claim generation")
	ttl := flag.Duration("ttl", time.Minute, "request expiry")
	timeout := flag.Duration("timeout", 20*time.Second, "http timeout")
	resolve := flag.String("resolve", "", "host:port:ip override (bypass local DNS for a tailnet FQDN)")
	dryRun := flag.Bool("dry-run", false, "print the envelope without sending")
	flag.Parse()

	if *url == "" || *goal == "" || *mutation == "" || *target == "" || *plan == "" ||
		*grant == "" || *owner == "" || *session == "" || *generation < 1 {
		fmt.Fprintln(os.Stderr, "url, goal, mutation, target, plan, grant, owner, session and generation are required")
		os.Exit(64)
	}
	key, err := effectgateway.ParseHexKey(strings.TrimSpace(os.Getenv("KC_EFFECT_KEY")))
	if err != nil {
		fmt.Fprintln(os.Stderr, "KC_EFFECT_KEY:", err)
		os.Exit(64)
	}

	req := effectgateway.Request{
		GoalID: *goal, MutationID: *mutation, Provider: *provider, Operation: *operation, Action: *action,
		TargetDigest: *target, PlanDigest: *plan, GrantDigest: *grant,
		Generation: *generation, Session: *session, Owner: *owner,
		ExpiresAt: time.Now().Add(*ttl).UTC(),
	}
	env := effectgateway.Sign(req, key)
	body, err := json.Marshal(env)
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
	if *dryRun {
		fmt.Println(string(body))
		return
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if *resolve != "" {
		parts := strings.SplitN(*resolve, ":", 3)
		if len(parts) != 3 {
			fmt.Fprintln(os.Stderr, "-resolve expects host:port:ip")
			os.Exit(64)
		}
		ipPort := net.JoinHostPort(parts[2], parts[1])
		transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, ipPort)
		}
	}
	client := &http.Client{Timeout: *timeout, Transport: transport}
	httpReq, err := http.NewRequest(http.MethodPost, *url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, "request:", err)
		os.Exit(1)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Println(`{"status":"TRANSPORT_ERROR"}`)
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	fmt.Printf("http=%d %s\n", resp.StatusCode, strings.TrimSpace(string(out)))
	if resp.StatusCode != http.StatusOK {
		os.Exit(3)
	}
}
