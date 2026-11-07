package verify

import (
	"context"
	"encoding/json"
	"net/http"
)

// probeResult is one billing probe line (JSON) on stdout.
type probeResult struct {
	Probe  string `json:"probe"`
	Status int    `json:"status"`
	Code   int    `json:"code,omitempty"`
	Result string `json:"result"` // ok | not_found | auth_fail | error
}

func runBillingProbes(ctx context.Context, c *cfHTTP, token []byte) ([]probeResult, bool, bool) {
	probes := []struct {
		name string
		path string
	}{
		{"subscriptions", "/accounts/" + c.account + "/billing/subscriptions"},
		{"billable_usage_v1", "/accounts/" + c.account + "/billing/v1/billable-usage"},
		{"user_billing_profile", "/user/billing/profile"},
		{"spend_policies", "/accounts/" + c.account + "/billing/spend-policies"},
	}
	var out []probeResult
	narrowPass := true
	anyWarn := false
	for _, p := range probes {
		pr := probeOne(ctx, c, token, p.name, p.path)
		out = append(out, pr)
		switch p.name {
		case "subscriptions", "billable_usage_v1":
			if pr.Result != "ok" {
				narrowPass = false
			}
		case "user_billing_profile":
			if pr.Result == "auth_fail" {
				anyWarn = true
			}
		case "spend_policies":
			if pr.Result != "ok" && pr.Result != "not_found" {
				anyWarn = true
			}
		}
	}
	return out, narrowPass, anyWarn
}

func probeOne(ctx context.Context, c *cfHTTP, token []byte, name, path string) probeResult {
	resp, body, err := c.bearerRequest(ctx, http.MethodGet, path, token)
	if err != nil {
		return probeResult{Probe: name, Status: 0, Result: "error"}
	}
	pr := probeResult{Probe: name, Status: resp.StatusCode}
	var parsed cfVerifyResponse
	_ = json.Unmarshal(body, &parsed)
	if len(parsed.Errors) > 0 {
		pr.Code = parsed.Errors[0].Code
	}
	switch {
	case resp.StatusCode == http.StatusOK && parsed.Success:
		pr.Result = "ok"
	case name == "spend_policies" && (resp.StatusCode == http.StatusNotFound || pr.Code == 1003):
		pr.Result = "not_found"
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		pr.Result = "auth_fail"
	default:
		if !parsed.Success && pr.Code == 1003 {
			pr.Result = "not_found"
		} else {
			pr.Result = "error"
		}
	}
	return pr
}
