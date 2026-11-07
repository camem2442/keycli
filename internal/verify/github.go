package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	defaultGHAPIBase   = "https://api.github.com"
	defaultGHProbeRepo = "camem2442/mock-exam-timer"
	defaultGHProbeBr   = "integration/member-records"
	maxStatusContexts  = 50
)

type ghHTTP struct {
	base   string
	client *http.Client
}

func newGHHTTP(client *http.Client) *ghHTTP {
	return &ghHTTP{base: defaultGHAPIBase, client: verificationClient(client)}
}

func probeRepoBranch() (repo, branch string) {
	repo = strings.TrimSpace(os.Getenv("GH_PROBE_REPO"))
	if repo == "" {
		repo = defaultGHProbeRepo
	}
	branch = strings.TrimSpace(os.Getenv("GH_PROBE_BRANCH"))
	if branch == "" {
		branch = defaultGHProbeBr
	}
	return repo, branch
}

func (g *ghHTTP) bearerRequest(ctx context.Context, method, path string, token []byte) (*http.Response, []byte, error) {
	reqURL := g.base + path
	req, err := http.NewRequestWithContext(ctx, method, reqURL, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if err != nil {
		return resp, nil, err
	}
	return resp, body, nil
}

func githubUser(ctx context.Context, g *ghHTTP, token []byte) (status int, login string, ok bool, err error) {
	resp, body, err := g.bearerRequest(ctx, http.MethodGet, "/user", token)
	if err != nil {
		return 0, "", false, err
	}
	status = resp.StatusCode
	if status != http.StatusOK {
		return status, "", false, nil
	}
	var parsed struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return status, "", false, err
	}
	return status, parsed.Login, true, nil
}

func runGitHubAgent(ctx context.Context, token []byte, client *http.Client) (*Outcome, error) {
	g := newGHHTTP(client)
	status, login, ok, err := githubUser(ctx, g, token)
	if err != nil {
		return nil, err
	}
	out := &Outcome{
		Provider:     string(ProviderGitHubAgent),
		VerifyStatus: status,
		Block:        !ok,
	}
	userLine, _ := json.Marshal(map[string]interface{}{
		"probe":  "user",
		"status": status,
		"result": ghUserResult(ok),
		"login":  login,
	})
	out.ProbeLines = append(out.ProbeLines, string(userLine))
	if !ok {
		return out, nil
	}

	repo, branch := probeRepoBranch()
	probes, block := runGitHubProbes(ctx, g, token, repo, branch)
	for _, pr := range probes {
		if pr.Probe == "protection" && (pr.Result == "not_verified" || pr.Result == "not_found") {
			out.Warn = true
		}
		b, _ := json.Marshal(pr)
		out.ProbeLines = append(out.ProbeLines, string(b))
	}
	if block {
		out.Block = true
	}
	return out, nil
}

func ghUserResult(ok bool) string {
	if ok {
		return "ok"
	}
	return "auth_fail"
}

type ghProbeLine struct {
	Probe     string   `json:"probe"`
	Status    int      `json:"status,omitempty"`
	Result    string   `json:"result"`
	Protected *bool    `json:"protected,omitempty"`
	Count     int      `json:"count,omitempty"`
	Contexts  []string `json:"contexts,omitempty"`
}

func runGitHubProbes(ctx context.Context, g *ghHTTP, token []byte, repo, branch string) ([]ghProbeLine, bool) {
	var out []ghProbeLine
	block := false

	branchPath := fmt.Sprintf("/repos/%s/branches/%s", repo, url.PathEscape(branch))
	resp, body, err := g.bearerRequest(ctx, http.MethodGet, branchPath, token)
	if err != nil {
		out = append(out, ghProbeLine{Probe: "branch", Result: "error"})
		return out, true
	}
	pr := ghProbeLine{Probe: "branch", Status: resp.StatusCode}
	if resp.StatusCode != http.StatusOK {
		pr.Result = "error"
		out = append(out, pr)
		return out, true
	}
	var branchMeta struct {
		Protected *bool `json:"protected"`
	}
	if err := json.Unmarshal(body, &branchMeta); err != nil || branchMeta.Protected == nil {
		pr.Result = "error"
		out = append(out, pr)
		return out, true
	}
	protected := *branchMeta.Protected
	pr.Protected = &protected
	pr.Result = "ok"
	out = append(out, pr)

	rulesPath := fmt.Sprintf("/repos/%s/rulesets", repo)
	resp, body, err = g.bearerRequest(ctx, http.MethodGet, rulesPath, token)
	if err != nil {
		out = append(out, ghProbeLine{Probe: "rulesets", Result: "error"})
		return out, true
	}
	rs := ghProbeLine{Probe: "rulesets", Status: resp.StatusCode}
	switch resp.StatusCode {
	case http.StatusOK:
		var rules []json.RawMessage
		if err := json.Unmarshal(body, &rules); err != nil || rules == nil {
			rs.Result = "error"
			block = true
		} else {
			rs.Result = "ok"
		}
	case http.StatusForbidden:
		rs.Result = "auth_fail"
		block = true
	default:
		rs.Result = "error"
		block = true
	}
	out = append(out, rs)
	if block {
		return out, block
	}

	if !protected {
		out = append(out, ghProbeLine{Probe: "protection", Result: "not_verified"})
		return out, false
	}

	protPath := branchPath + "/protection"
	resp, body, err = g.bearerRequest(ctx, http.MethodGet, protPath, token)
	if err != nil {
		out = append(out, ghProbeLine{Probe: "protection", Result: "error"})
		return out, true
	}
	pp := ghProbeLine{Probe: "protection", Status: resp.StatusCode}
	switch resp.StatusCode {
	case http.StatusOK:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(body, &object); err != nil || object == nil {
			pp.Result = "error"
			block = true
			break
		}
		contexts, parseErr := protectionContexts(body)
		if parseErr != nil {
			pp.Result = "error"
			block = true
			break
		}
		pp.Result = "ok"
		if len(contexts) > maxStatusContexts {
			contexts = contexts[:maxStatusContexts]
		}
		pp.Contexts = contexts
		pp.Count = len(contexts)
	case http.StatusNotFound:
		// A ruleset can protect a branch without a classic protection rule.
		pp.Result = "not_found"
	case http.StatusForbidden, http.StatusUnauthorized:
		pp.Result = "auth_fail"
		block = true
	default:
		pp.Result = "error"
		block = true
	}
	out = append(out, pp)
	return out, block
}

func protectionContexts(body []byte) ([]string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil || object == nil {
		return nil, fmt.Errorf("invalid protection response")
	}
	raw, ok := object["required_status_checks"]
	if !ok {
		return nil, fmt.Errorf("required status checks missing")
	}
	if string(raw) == "null" {
		return nil, nil
	}
	var parsed struct {
		Contexts []string `json:"contexts"`
		Checks   []struct {
			Context string `json:"context"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(parsed.Contexts)+len(parsed.Checks))
	seen := make(map[string]bool)
	add := func(name string) error {
		if name == "" {
			return fmt.Errorf("empty check context")
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		return nil
	}
	for _, name := range parsed.Contexts {
		if err := add(name); err != nil {
			return nil, err
		}
	}
	for _, check := range parsed.Checks {
		if err := add(check.Context); err != nil {
			return nil, err
		}
	}
	return out, nil
}
