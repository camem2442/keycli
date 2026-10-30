package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/doctor"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/middleware"
)

// GetToolDefinitions returns all supported MCP tools.
func GetToolDefinitions() []Tool {
	return []Tool{
		{
			Name:        "list_secrets",
			Description: "List available secret names and metadata. Never returns secret values.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"all": {
						Type:        "boolean",
						Description: "Include secret names denied by agent policy (default: false)",
					},
					"stale": {
						Type:        "boolean",
						Description: "Annotate secrets that changed since last export (default: false)",
					},
				},
			},
		},
		{
			Name:        "get_secret",
			Description: "Retrieve a secret value by its name or alias. Respects audit logging and agent policy allowlist.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"name": {
						Type:        "string",
						Description: "The name or alias of the secret to retrieve (e.g., 'github', 'OPENAI_API_KEY')",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "policy_check",
			Description: "Check if the agent is permitted to access a given secret name under the active policy.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]PropertyDef{
					"name": {
						Type:        "string",
						Description: "The secret name to check",
					},
					"agent": {
						Type:        "string",
						Description: "Optional agent profile override (defaults to detected agent)",
					},
				},
				Required: []string{"name"},
			},
		},
		{
			Name:        "doctor",
			Description: "Diagnose keycli setup, identities, and backends. Never outputs secret values.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]PropertyDef{},
			},
		},
	}
}

// ExecuteTool handles tool execution by name.
func ExecuteTool(ctx context.Context, a *app.App, name string, argsRaw json.RawMessage) (*CallToolResult, error) {
	switch name {
	case "list_secrets":
		var args struct {
			All   bool `json:"all"`
			Stale bool `json:"stale"`
		}
		if len(argsRaw) > 0 {
			_ = json.Unmarshal(argsRaw, &args)
		}

		items, err := a.Resolver.List(ctx)
		if err != nil {
			return errorResult(fmt.Sprintf("Failed to list secrets: %v", err)), nil
		}
		if args.Stale {
			items = a.AnnotateStale(ctx, items)
		}

		sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })

		type secretSummary struct {
			Name         string   `json:"name"`
			Aliases      []string `json:"aliases,omitempty"`
			Backend      string   `json:"backend"`
			Source       string   `json:"source,omitempty"`
			Note         string   `json:"note,omitempty"`
			Expires      *string  `json:"expires,omitempty"`
			StaleExport  bool     `json:"stale_export,omitempty"`
			PolicyDenied bool     `json:"policy_denied,omitempty"`
		}

		var summary []secretSummary
		for _, it := range items {
			if it.PolicyDenied && !args.All {
				continue
			}
			summary = append(summary, secretSummary{
				Name:         it.Name,
				Aliases:      it.Aliases,
				Backend:      it.Backend,
				Source:       it.Source,
				Note:         it.Note,
				Expires:      it.Expires,
				StaleExport:  it.StaleExport,
				PolicyDenied: it.PolicyDenied,
			})
		}

		b, err := json.MarshalIndent(map[string]any{
			"v":       1,
			"secrets": summary,
		}, "", "  ")
		if err != nil {
			return errorResult(fmt.Sprintf("JSON marshal error: %v", err)), nil
		}

		return textResult(string(b)), nil

	case "get_secret":
		var args struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.Name == "" {
			return errorResult("Argument 'name' is required"), nil
		}

		val, _, src, err := a.Resolver.GetWithSource(ctx, args.Name)
		if err != nil {
			if kcerrors.Code(err) == kcerrors.ExitDenied {
				return errorResult(fmt.Sprintf("Access denied by policy for secret %q. Update agent allowlist in kc config.", args.Name)), nil
			}
			if kcerrors.Code(err) == kcerrors.ExitNotFound {
				return errorResult(fmt.Sprintf("Secret %q not found. Ask user to run 'kc set %s'.", args.Name, args.Name)), nil
			}
			return errorResult(fmt.Sprintf("Failed to get secret %q: %v", args.Name, err)), nil
		}

		res := string(val)
		if src != "" {
			// Do not log or leak value, just return value
		}
		return textResult(res), nil

	case "policy_check":
		var args struct {
			Name  string `json:"name"`
			Agent string `json:"agent"`
		}
		if err := json.Unmarshal(argsRaw, &args); err != nil || args.Name == "" {
			return errorResult("Argument 'name' is required"), nil
		}

		agent := args.Agent
		if agent == "" {
			agent = middleware.DetectAgent()
		}

		p := middleware.NewPolicy(a.Config, agent)
		canon := a.Resolver.CanonicalName(args.Name)

		if !middleware.PolicyEnabled(a.Config) {
			return textResult(fmt.Sprintf("Policy middleware not enabled: agent %q is allowed to read %q (all reads permitted).", agent, canon)), nil
		}

		if p.Allowed(canon) {
			return textResult(fmt.Sprintf("Allowed: agent %q is permitted to read %q.", agent, canon)), nil
		}

		return errorResult(fmt.Sprintf("Denied: agent %q is NOT permitted to read %q under active policy. Ask user to update allowlist.", agent, canon)), nil

	case "doctor":
		rep := doctor.Collect(ctx, a)
		b, err := doctor.MarshalJSON(rep)
		if err != nil {
			return errorResult(fmt.Sprintf("Doctor error: %v", err)), nil
		}
		return textResult(string(b)), nil

	default:
		return errorResult(fmt.Sprintf("Unknown tool: %s", name)), nil
	}
}

func textResult(text string) *CallToolResult {
	return &CallToolResult{
		Content: []ContentItem{
			{
				Type: "text",
				Text: text,
			},
		},
		IsError: false,
	}
}

func errorResult(msg string) *CallToolResult {
	return &CallToolResult{
		Content: []ContentItem{
			{
				Type: "text",
				Text: msg,
			},
		},
		IsError: true,
	}
}
