package opencode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	ProviderID     = "vertex-byok"
	ConfigFileName = "opencode.json"
	EnvAPIKey      = "PROXY_API_KEY"
)

// DefaultConfigPath is ~/.config/opencode/opencode.json
func DefaultConfigPath(home string) string {
	return filepath.Join(home, ".config", "opencode", ConfigFileName)
}

// VertexProxyPort reads PROXY_PORT from cursor-vertex-proxy config.env
func VertexProxyPort(home string) (int, error) {
	path := filepath.Join(home, ".config", "cursor-vertex-proxy", "config.env")
	data, err := os.ReadFile(path)
	if err != nil {
		return 8787, fmt.Errorf("vertex proxy config: %w (default port 8787)", err)
	}
	re := regexp.MustCompile(`(?m)^PROXY_PORT=(\d+)\s*$`)
	m := re.FindSubmatch(data)
	if m == nil {
		return 8787, nil
	}
	var port int
	if _, err := fmt.Sscanf(string(m[1]), "%d", &port); err != nil || port <= 0 {
		return 8787, nil
	}
	return port, nil
}

// ProviderBlock returns the OpenCode v1 custom provider map for vertex-byok.
func ProviderBlock(baseURL string) map[string]any {
	return map[string]any{
		ProviderID: map[string]any{
			"npm":  "@ai-sdk/openai-compatible",
			"name": "Vertex BYOK (local proxy)",
			"options": map[string]any{
				"baseURL": baseURL,
				"apiKey":  "{env:" + EnvAPIKey + "}",
			},
			"models": map[string]any{
				"google/gemini-3.7-flash": map[string]any{
					"name": "Gemini 3.7 Flash (Vertex)",
				},
				"google/gemini-3.6-flash": map[string]any{
					"name": "Gemini 3.6 Flash (Vertex)",
				},
				"google/gemini-3.1-pro-preview": map[string]any{
					"name": "Gemini 3.1 Pro (Vertex)",
				},
			},
		},
	}
}

// MergeConfigFile reads path (or creates minimal doc), merges provider.vertex-byok, writes unless dryRun.
func MergeConfigFile(path string, baseURL string, dryRun bool) ([]byte, error) {
	var root map[string]any
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	} else if os.IsNotExist(err) {
		root = map[string]any{
			"$schema": "https://opencode.ai/config.json",
		}
	} else {
		return nil, err
	}
	prov, _ := root["provider"].(map[string]any)
	if prov == nil {
		prov = map[string]any{}
	}
	block := ProviderBlock(baseURL)[ProviderID].(map[string]any)
	prov[ProviderID] = block
	root["provider"] = prov

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	out = append(out, '\n')
	if dryRun {
		return out, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return nil, err
	}
	return out, nil
}

// WireOpencodeVertex writes merged config for local vertex-byok proxy.
func WireOpencodeVertex(home string, configPath string, dryRun bool) ([]byte, string, error) {
	if strings.TrimSpace(configPath) == "" {
		configPath = DefaultConfigPath(home)
	}
	port, err := VertexProxyPort(home)
	if err != nil {
		// still use default port from error path
	}
	baseURL := fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	out, err := MergeConfigFile(configPath, baseURL, dryRun)
	if err != nil {
		return nil, "", err
	}
	return out, configPath, nil
}
