package cli

import (
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/plugin"
	"github.com/spf13/cobra"
)

func newPluginCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "External backend plugins",
	}
	cmd.AddCommand(newPluginListCmd(a))
	return cmd
}

func newPluginListCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List discovered kc-backend-* plugins and enabled state",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := plugin.List(cmd.Context(), a.Home, a.Config, nil)
			if err != nil {
				return err
			}
			if asJSON {
				b, err := plugin.MarshalJSON(entries)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(b)
				_, _ = cmd.OutOrStdout().Write([]byte("\n"))
				return err
			}
			plugin.PrintHuman(os.Stdout, entries)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return cmd
}
