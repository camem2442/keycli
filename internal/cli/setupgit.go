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
	cmd := &cobra.Command{
		Use:   "setup-git",
		Short: "Configure git (and document gh) to use kc for GitHub",
		RunE: func(cmd *cobra.Command, args []string) error {
			kcPath, err := gitsetup.ResolveKCPath()
			if err != nil {
				return err
			}
			plan := gitsetup.BuildPlan(kcPath)
			plan.Current, _ = gitsetup.ReadCurrentHelper()
			if undo {
				if dryRun {
					fmt.Fprintf(cmd.OutOrStdout(), "would run: git config --global --unset credential.https://github.com.helper\n")
					return nil
				}
				if err := gitsetup.Undo(kcPath); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "kc: removed git credential helper for github.com")
				return nil
			}
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "git config --global credential.https://github.com.helper %q\n", plan.HelperValue)
				fmt.Fprintln(cmd.OutOrStdout())
				fmt.Fprintln(cmd.OutOrStdout(), plan.GhSnippet)
				return nil
			}
			if err := gitsetup.Apply(plan); err != nil {
				return fmt.Errorf("%w: git config failed (is git installed?)", kcerrors.ErrUsage)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "kc: git credential helper configured for https://github.com")
			fmt.Fprintln(cmd.OutOrStdout(), plan.GhSnippet)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print commands without changing git config")
	cmd.Flags().BoolVar(&undo, "undo", false, "Remove kc git credential helper")
	_ = a
	return cmd
}
