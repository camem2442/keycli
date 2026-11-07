package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
)

// Outcome is the set-time verification result for stdout and set abort logic.
type Outcome struct {
	Provider     string
	VerifyStatus int
	CFCodes      []int
	ProbeLines   []string // JSON objects, one per line
	Block        bool
	Warn         bool
}

// Run executes provider verification for kc set. token must stay in memory only.
func Run(ctx context.Context, name, note string, token []byte, client *http.Client) (*Outcome, error) {
	p, ok := DetectProvider(name, note)
	if !ok {
		return nil, nil
	}
	switch p {
	case ProviderCloudflareAgent:
		return runCloudflareAgent(ctx, token, client)
	case ProviderCloudflareBillingRead:
		return runCloudflareBilling(ctx, token, client)
	case ProviderGitHubPAT:
		return nil, nil
	default:
		return nil, nil
	}
}

func runCloudflareAgent(ctx context.Context, token []byte, client *http.Client) (*Outcome, error) {
	c := newCFHTTP(client)
	status, codes, ok, err := tokenVerify(ctx, c, token)
	if err != nil {
		return nil, err
	}
	out := &Outcome{
		Provider:     string(ProviderCloudflareAgent),
		VerifyStatus: status,
		CFCodes:      codes,
		Block:        !ok,
	}
	if ok {
		n, err := accountsCount(ctx, c, token)
		if err != nil {
			out.Warn = true
			line, _ := json.Marshal(map[string]interface{}{
				"probe":  "accounts",
				"result": "error",
			})
			out.ProbeLines = append(out.ProbeLines, string(line))
		} else {
			line, _ := json.Marshal(map[string]interface{}{
				"probe":  "accounts",
				"count":  n,
				"result": "ok",
			})
			out.ProbeLines = append(out.ProbeLines, string(line))
		}
	}
	return out, nil
}

func runCloudflareBilling(ctx context.Context, token []byte, client *http.Client) (*Outcome, error) {
	c := newCFHTTP(client)
	status, codes, ok, err := tokenVerify(ctx, c, token)
	if err != nil {
		return nil, err
	}
	out := &Outcome{
		Provider:     string(ProviderCloudflareBillingRead),
		VerifyStatus: status,
		CFCodes:      codes,
		Block:        !ok,
	}
	if !ok {
		return out, nil
	}
	probes, narrowPass, warn := runBillingProbes(ctx, c, token)
	for _, pr := range probes {
		b, _ := json.Marshal(pr)
		out.ProbeLines = append(out.ProbeLines, string(b))
	}
	if !narrowPass {
		out.Block = true
	}
	out.Warn = warn && narrowPass
	return out, nil
}

// Print writes contract stdout lines (never includes secret bytes).
func (o *Outcome) Print(w *os.File) {
	if o == nil {
		return
	}
	fmt.Fprintf(w, "provider %s verify_status %d\n", o.Provider, o.VerifyStatus)
	if len(o.CFCodes) > 0 {
		fmt.Fprintf(w, "cf_codes %v\n", o.CFCodes)
	}
	for _, line := range o.ProbeLines {
		fmt.Fprintln(w, line)
	}
}

// ErrVerifyFailed is returned when verification blocks store (no --force).
var ErrVerifyFailed = errors.New("set-time verify failed")
