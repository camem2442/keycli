package cli

import (
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/interactive"
	"github.com/spf13/cobra"
)

func newInteractiveCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:     "interactive",
		Aliases: []string{"i", "ui"},
		Short:   "Launch interactive TUI secret manager",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return interactive.RunManager(cmd.Context(), a, clipboardCopyWithAutoClear)
		},
	}
}
