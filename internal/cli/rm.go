package cli

import (
	"github.com/camem2442/keycli/internal/app"
	"github.com/spf13/cobra"
)

func newRmCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "rm NAME",
		Short: "Delete a secret",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.Resolver.Delete(cmd.Context(), args[0])
		},
	}
}
