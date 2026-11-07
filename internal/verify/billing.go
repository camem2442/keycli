package verify

import (
	"context"
	"encoding/json"
	"net/http"
)

// probeResult is a redacted billing probe line: no token or response body.
type probeResult struct {
	Probe  string `json:"probe"`
	Status int    `json:"status"`
	Code   int    `json:"code,omitempty"`
	Result string `json:"result"` // ok | not_verified | auth_fail | error
}

func runBillingProbes(ctx context.Context, c *cfHTTP, token []byte) ([]probeResult, bool, bool) {
	probes := []struct {
		name string
		path string
	}{
		{"subscriptions", "/accounts/" + c.account + "/subscriptions"},
		{"billable_usage_v1", "/accounts/" + c.account + "/billable-usage"},
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
		default:
			if pr.Result != "ok" {
				anyWarn = true
			}
		}
	}
	// The narrow pass is permission/response-shape evidence, never Free-plan
	// confirmation, spend-guard admission or deployment authorization.
	return out, narrowPass, anyWarn
}

func probeOne(ctx context.Context, c *cfHTTP, token []byte, name, path string) probeResult {
	resp, body, err := c.bearerRequest(ctx, http.MethodGet, path, token)
	if err != nil {
		return probeResult{Probe: name, Status: 0, Result: "error"}
	}
	pr := probeResult{Probe: name, Status: resp.StatusCode}
	var parsed cfVerifyResponse
	if json.Unmarshal(body, &parsed) != nil {
		pr.Result = "error"
		return pr
	}
	if len(parsed.Errors) > 0 {
		pr.Code = parsed.Errors[0].Code
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		pr.Result = "auth_fail"
	case resp.StatusCode == http.StatusOK && parsed.Success && len(parsed.Errors) == 0:
		if validBillingResultShape(name, body) {
			pr.Result = "ok"
		} else {
			pr.Result = "error"
		}
	case name == "spend_policies" && (resp.StatusCode == http.StatusNotFound || pr.Code == 1003):
		// GET list returns 200+[] for an empty collection. A 404 is NOT proof
		// of an empty policy set or of access to spend controls.
		pr.Result = "not_verified"
	default:
		pr.Result = "error"
	}
	return pr
}

func validBillingResultShape(name string, body []byte) bool {
	var envelope struct {
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil || len(envelope.Result) == 0 {
		return false
	}
	switch name {
	case "subscriptions", "billable_usage_v1", "spend_policies":
		var list []json.RawMessage
		return json.Unmarshal(envelope.Result, &list) == nil && list != nil
	case "user_billing_profile":
		var object map[string]json.RawMessage
		return json.Unmarshal(envelope.Result, &object) == nil && object != nil
	default:
		return false
	}
}
