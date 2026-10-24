package cli

import (
	"os"

	"github.com/camem2442/keycli/internal/agebundle"
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/spf13/cobra"
)

func newImportCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "import",
		Short: "Import an age bundle from stdin into the file store",
		RunE: func(cmd *cobra.Command, args []string) error {
			fs, err := a.FileStore(cmd.Context())
			if err != nil {
				return err
			}
			id, err := fs.LoadIdentityForImport()
			if err != nil {
				return err
			}
			secrets, err := agebundle.Decrypt(os.Stdin, id)
			if err != nil {
				return err
			}
			for name, val := range secrets {
				if err := a.Resolver.Set(cmd.Context(), name, val, backend.Meta{}); err != nil {
					return err
				}
			}
			return nil
		},
	}
}
