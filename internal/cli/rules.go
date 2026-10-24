package cli

import (
	"fmt"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/rules"
	"github.com/spf13/cobra"
)

func newRulesCmd(a *app.App) *cobra.Command {
	var target string
	var writePath string
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Emit agent instruction block for kc usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := rules.Target(target)
			block := rules.Block(t)
			if writePath != "" {
				if err := rules.WriteFile(writePath, t); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "kc: updated %s\n", writePath)
				return nil
			}
			_, err := fmt.Fprint(cmd.OutOrStdout(), block)
			return err
		},
	}
	cmd.Flags().StringVar(&target, "target", "agents", "cursor|claude|codex|agents")
	cmd.Flags().StringVar(&writePath, "write", "", "Insert/update block in file path")
	_ = a
	return cmd
}
