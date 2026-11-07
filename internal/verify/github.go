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
	"time"
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
	base := strings.TrimRight(os.Getenv("GH_API_BASE"), "/")
	if base == "" {
		base = defaultGHAPIBase
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &ghHTTP{base: base, client: client}
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
		Protected bool `json:"protected"`
	}
	_ = json.Unmarshal(body, &branchMeta)
	protected := branchMeta.Protected
	pr.Protected = &protected
	pr.Result = "ok"
	out = append(out, pr)

	rulesPath := fmt.Sprintf("/repos/%s/rulesets", repo)
	resp, _, err = g.bearerRequest(ctx, http.MethodGet, rulesPath, token)
	if err != nil {
		out = append(out, ghProbeLine{Probe: "rulesets", Result: "error"})
		return out, true
	}
	rs := ghProbeLine{Probe: "rulesets", Status: resp.StatusCode}
	switch resp.StatusCode {
	case http.StatusOK:
		rs.Result = "ok"
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
		return out, false
	}

	protPath := branchPath + "/protection"
	resp, body, err = g.bearerRequest(ctx, http.MethodGet, protPath, token)
	if err != nil {
		out = append(out, ghProbeLine{Probe: "protection", Result: "error"})
		return out, false
	}
	pp := ghProbeLine{Probe: "protection", Status: resp.StatusCode}
	switch resp.StatusCode {
	case http.StatusOK:
		pp.Result = "ok"
		contexts := protectionContexts(body)
		if len(contexts) > maxStatusContexts {
			contexts = contexts[:maxStatusContexts]
		}
		pp.Contexts = contexts
		pp.Count = len(contexts)
	case http.StatusNotFound:
		pp.Result = "not_found"
	default:
		pp.Result = "error"
	}
	out = append(out, pp)
	return out, block
}

func protectionContexts(body []byte) []string {
	var parsed struct {
		RequiredStatusChecks *struct {
			Contexts []string `json:"contexts"`
		} `json:"required_status_checks"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil
	}
	if parsed.RequiredStatusChecks == nil {
		return nil
	}
	return append([]string(nil), parsed.RequiredStatusChecks.Contexts...)
}
