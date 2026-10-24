package cli

import (
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/doctor"
	"github.com/spf13/cobra"
)

func newDoctorCmd(a *app.App) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose kc setup (never prints secret values)",
		RunE: func(cmd *cobra.Command, args []string) error {
			rep := doctor.Collect(cmd.Context(), a)
			if asJSON {
				b, err := doctor.MarshalJSON(rep)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(b)
				_, _ = cmd.OutOrStdout().Write([]byte("\n"))
				return err
			}
			doctor.PrintHuman(os.Stdout, rep)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return cmd
}
