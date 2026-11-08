package cli

import (
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/middleware"
	"github.com/spf13/cobra"
)

func newPolicyCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Agent allowlist policy (guardrail)",
	}
	cmd.AddCommand(newPolicyCheckCmd(a))
	cmd.AddCommand(newPolicyShowCmd(a))
	cmd.AddCommand(newPolicyValidateCmd(a))
	return cmd
}

func newPolicyCheckCmd(a *app.App) *cobra.Command {
	var agent string
	cmd := &cobra.Command{
		Use:   "check NAME",
		Short: "Check whether the agent may read NAME",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if agent == "" {
				agent = middleware.DetectAgent()
			}
			p := middleware.NewPolicy(a.Config, agent)
			canon := a.Resolver.CanonicalName(args[0])
			if err := a.Config.ValidatePolicyPatterns(); err != nil {
				fmt.Fprintf(os.Stderr, "kc: %v\n", err)
				return err
			}
			if !middleware.PolicyEnabled(a.Config) {
				if middleware.HasRules(a.Config) {
					fmt.Fprintf(os.Stderr, "kc: policy rules configured but \"policy\" is not in middleware; reads are not enforced\n")
				} else {
					fmt.Fprintf(os.Stderr, "kc: no policy configured (all reads allowed)\n")
				}
				return nil
			}
			d := p.Evaluate(canon)
			if d.Allowed {
				fmt.Fprintf(os.Stderr, "kc: agent %q may read %q\n", agent, canon)
				return nil
			}
			if d.Rule != "" {
				fmt.Fprintf(os.Stderr, "kc: agent %q may not read %q (%s %q)\n", agent, canon, d.Reason, d.Rule)
			} else {
				fmt.Fprintf(os.Stderr, "kc: agent %q may not read %q (%s)\n", agent, canon, d.Reason)
			}
			return kcerrors.ErrDenied
		},
	}
	cmd.Flags().StringVar(&agent, "agent", "", "Agent id to evaluate (default: detected)")
	return cmd
}
