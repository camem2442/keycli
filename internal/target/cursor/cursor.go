package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/resolve"
	"github.com/camem2442/keycli/internal/target"
	"github.com/camem2442/keycli/internal/testhooks"
)

const defaultAPIBase = "https://api.cursor.com"

// Options configure a cursor push.
type Options struct {
	EnvironmentID string
	Repos         []string
	SecretType    string // runtime_secret | environment_variable
	DryRun        bool
	APIKey        string
	HTTPClient    *http.Client
	APIBase       string
}

// Push sends secrets to Cursor Cloud Agents.
// API: PUT https://api.cursor.com/v1/environments/{id}/secrets/{name}
// Auth: Basic (-u API_KEY:). Body: {"value","type","repos"}.
// See https://cursor.com/docs/cloud-agent/api/endpoints (Set An Environment Secret).
func Push(ctx context.Context, opts Options, resolver *resolve.Resolver, cfg config.Config, secrets []target.Secret) error {
	resolved, err := resolveOpts(ctx, opts, resolver, cfg)
	if err != nil {
		return err
	}
	for _, sec := range secrets {
		if err := validateValue(sec.Value); err != nil {
			return fmt.Errorf("%s: %w", sec.Name, err)
		}
		if resolved.DryRun {
			fmt.Fprintf(os.Stderr, "kc: [dry-run] cursor push %s → env %s\n", sec.Name, resolved.EnvironmentID)
			continue
		}
		if err := putSecret(ctx, resolved, sec); err != nil {
			return err
		}
	}
	return nil
}

// ListEnvironmentSecrets names secrets in the environment (no values).
func ListEnvironmentSecrets(ctx context.Context, opts Options, resolver *resolve.Resolver, cfg config.Config) ([]string, error) {
	resolved, err := resolveOpts(ctx, opts, resolver, cfg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolved.APIBase+"/v1/environments/"+url.PathEscape(resolved.EnvironmentID)+"/secrets", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(resolved.APIKey, "")
	resp, err := resolved.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}
	var body struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var names []string
	for _, it := range body.Items {
		if _, ok := seen[it.Name]; ok {
			continue
		}
		seen[it.Name] = struct{}{}
		names = append(names, it.Name)
	}
	return names, nil
}

func validateValue(v []byte) error {
	if len(v) < 1 || len(v) > 4096 {
		return fmt.Errorf("value must be 1–4096 bytes (got %d)", len(v))
	}
	if !utf8.Valid(v) {
		return fmt.Errorf("value must be UTF-8 text")
	}
	return nil
}

func putSecret(ctx context.Context, opts Options, sec target.Secret) error {
	name := strings.TrimSpace(sec.Name)
	u := opts.APIBase + "/v1/environments/" + url.PathEscape(opts.EnvironmentID) + "/secrets/" + url.PathEscape(name)
	body := map[string]interface{}{
		"value": string(sec.Value),
	}
	if opts.SecretType != "" {
		body["type"] = opts.SecretType
	}
	if len(opts.Repos) > 0 {
		body["repos"] = opts.Repos
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(opts.APIKey, "")
	resp, err := opts.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		return nil
	case http.StatusConflict:
		return parseConflict(resp)
	default:
		return apiError(resp)
	}
}

func parseConflict(resp *http.Response) error {
	var errBody struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Details []struct {
			ID    string   `json:"id"`
			Repos []string `json:"repos"`
		} `json:"details"`
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &errBody)
	msg := errBody.Message
	if msg == "" {
		msg = string(raw)
	}
	if errBody.Error == "secret_name_ambiguous" || strings.Contains(msg, "secret_name_ambiguous") {
		var parts []string
		for _, d := range errBody.Details {
			parts = append(parts, fmt.Sprintf("id=%s repos=%v", d.ID, d.Repos))
		}
		return fmt.Errorf("cursor: secret_name_ambiguous — 동일 이름의 여러 버전이 있습니다. PUT에 ?id=<version>을 지정하세요 (%s)", strings.Join(parts, "; "))
	}
	if errBody.Error == "secret_limit_reached" || strings.Contains(msg, "secret_limit_reached") {
		return fmt.Errorf("cursor: 환경 시크릿 상한(1000개)에 도달했습니다")
	}
	return fmt.Errorf("cursor: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(msg))
}

func apiError(resp *http.Response) error {
	raw, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(raw))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	return fmt.Errorf("cursor: HTTP %d: %s", resp.StatusCode, msg)
}

func resolveOpts(ctx context.Context, opts Options, resolver *resolve.Resolver, cfg config.Config) (Options, error) {
	out := opts
	if out.APIBase == "" {
		out.APIBase = defaultAPIBase
	}
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	if out.EnvironmentID == "" {
		out.EnvironmentID = strings.TrimSpace(os.Getenv("KC_CURSOR_ENVIRONMENT_ID"))
	}
	if out.APIKey == "" && resolver != nil {
		val, _, err := resolver.Get(ctx, "cursor-api-key")
		if err == nil {
			out.APIKey = strings.TrimSpace(string(val))
		}
	}
	if out.APIKey == "" {
		out.APIKey = strings.TrimSpace(os.Getenv("KC_CURSOR_API_KEY"))
	}
	if out.EnvironmentID == "" {
		return Options{}, fmt.Errorf("cursor: environment id required (KC_CURSOR_ENVIRONMENT_ID or --env-id)")
	}
	if out.APIKey == "" {
		return Options{}, fmt.Errorf("cursor: API key required (kc에 cursor-api-key 저장 또는 KC_CURSOR_API_KEY)")
	}
	if b := testhooks.Env(cfg, "KC_CURSOR_API_BASE"); b != "" {
		out.APIBase = strings.TrimRight(b, "/")
	}
	if b := os.Getenv("KC_CURSOR_API_BASE"); b != "" {
		out.APIBase = strings.TrimRight(b, "/")
	}
	if out.SecretType == "" {
		out.SecretType = "runtime_secret"
	}
	return out, nil
}
