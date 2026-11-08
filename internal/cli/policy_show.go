package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/middleware"
	"github.com/spf13/cobra"
)

func newPolicyShowCmd(a *app.App) *cobra.Command {
	var agent string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the effective allow/deny rules for an agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent == "" {
				agent = middleware.DetectAgent()
			}
			spec, source := middleware.EffectiveRules(a.Config, agent)
			enforced := middleware.PolicyEnabled(a.Config)
			allow := append([]string{}, spec.Allow...)
			deny := append([]string{}, spec.Deny...)

			if asJSON {
				// Keep policy show usable for malformed rules: validation is a
				// diagnostic step, and an invalid policy must have no digest.
				digest, _ := middleware.EffectivePolicyDigest(a.Config, agent)
				payload := struct {
					V            int      `json:"v"`
					Agent        string   `json:"agent"`
					Enforced     bool     `json:"enforced"`
					Source       string   `json:"source"`
					Allow        []string `json:"allow"`
					Deny         []string `json:"deny"`
					PolicyDigest string   `json:"policy_digest,omitempty"`
				}{V: 1, Agent: agent, Enforced: enforced, Source: source, Allow: allow, Deny: deny, PolicyDigest: digest}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(payload)
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "agent: %s\n", agent)
			fmt.Fprintf(out, "enforced: %t\n", enforced)
			fmt.Fprintf(out, "source: %s\n", source)
			printPatterns(out, "allow", allow)
			printPatterns(out, "deny", deny)
			return nil
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Agent id to inspect (default: detected)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output (schema v1)")
	return cmd
}

func printPatterns(w io.Writer, label string, patterns []string) {
	if len(patterns) == 0 {
		fmt.Fprintf(w, "%s: (none)\n", label)
		return
	}
	fmt.Fprintf(w, "%s:\n", label)
	for _, p := range patterns {
		fmt.Fprintf(w, "  - %s\n", p)
	}
}

func newPolicyValidateCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate every allow/deny pattern (all errors, not just the first)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			errs := a.Config.PolicyPatternErrors()
			if asJSON {
				payload := struct {
					V      int      `json:"v"`
					OK     bool     `json:"ok"`
					Agents int      `json:"agents"`
					Errors []string `json:"errors"`
				}{V: 1, OK: len(errs) == 0, Agents: len(a.Config.Policy.Agent), Errors: append([]string{}, errs...)}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				if err := enc.Encode(payload); err != nil {
					return err
				}
			} else if len(errs) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "kc: policy ok (%d agent rules)\n", len(a.Config.Policy.Agent))
			} else {
				for _, e := range errs {
					fmt.Fprintf(cmd.ErrOrStderr(), "kc: %s\n", e)
				}
			}
			if len(errs) > 0 {
				return fmt.Errorf("%d policy pattern error(s)", len(errs))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output (schema v1)")
	return cmd
}
