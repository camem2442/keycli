package cli

import (
	"fmt"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/gitsetup"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

func newSetupGitCmd(a *app.App) *cobra.Command {
	var dryRun bool
	var undo bool
	var host string
	cmd := &cobra.Command{
		Use:   "setup-git",
		Short: "Configure git to use kc for a mapped host (default github.com)",
		RunE: func(cmd *cobra.Command, args []string) error {
			kcPath, err := gitsetup.ResolveKCPath()
			if err != nil {
				return err
			}
			plan := gitsetup.BuildPlan(kcPath, host)
			plan.Current, _ = gitsetup.ReadCurrentHelper(plan.Host)
			if undo {
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "would run: git config --global --unset %s\n", plan.Key)
					return nil
				}
				if err := gitsetup.Undo(kcPath, plan.Host); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "kc: removed git credential helper for %s\n", plan.Host)
				return nil
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "git config --global %s %q\n", plan.Key, plan.HelperValue)
				if spec, added := a.Config.EnsureGitHost(plan.Host); added {
					fmt.Fprintf(cmd.OutOrStdout(), "would record [git.%q] secret = %q in %s\n", plan.Host, spec.Secret, a.ConfigPath)
				}
				if plan.Host == gitsetup.DefaultHost {
					fmt.Fprintln(cmd.OutOrStdout())
					fmt.Fprintln(cmd.OutOrStdout(), plan.GhSnippet)
				}
				return nil
			}
			spec, added := a.Config.EnsureGitHost(plan.Host)
			if added {
				if err := a.SaveConfig(); err != nil {
					return err
				}
				if err := a.Reload(cmd.Context()); err != nil {
					return err
				}
			}
			if err := gitsetup.Apply(plan); err != nil {
				return fmt.Errorf("%w: git config failed (is git installed?)", kcerrors.ErrUsage)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "kc: git credential helper configured for https://%s\n", plan.Host)
			if added {
				fmt.Fprintf(cmd.OutOrStdout(), "kc: recorded [git.%q] (secret %q) in %s\n", plan.Host, spec.Secret, a.ConfigPath)
			}
			if plan.Host == gitsetup.DefaultHost {
				fmt.Fprintln(cmd.OutOrStdout(), plan.GhSnippet)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print commands without changing git config")
	cmd.Flags().BoolVar(&undo, "undo", false, "Remove kc git credential helper for the host")
	cmd.Flags().StringVar(&host, "host", gitsetup.DefaultHost, "Git host to configure (e.g. origin.cursor.com)")
	return cmd
}
