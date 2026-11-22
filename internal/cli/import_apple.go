package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/applecsv"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newImportAppleCmd is intentionally human-operated and one-account-only.
// It never emits credential data and does not expose passkey or OTP imports.
func newImportAppleCmd(a *app.App) *cobra.Command {
	var file, site, name string
	var write, acceptRisk bool
	cmd := &cobra.Command{
		Use:   "import-apple",
		Short: "Validate one Apple Passwords CSV record (read-only by default)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" || site == "" || name == "" {
				return fmt.Errorf("--file, --site, and --name are required")
			}
			if name != "web."+strings.ToLower(site) {
				return fmt.Errorf("--name must equal web.<site> (only the site name is displayed)")
			}
			// Pass explicit site into the parser; a CSV URL never grants itself access.
			credential, err := applecsv.ReadOne(file, site)
			if err != nil {
				return err
			}
			if !write {
				fmt.Fprintln(cmd.OutOrStdout(), "validated one Apple password record; no secret stored or printed")
				return nil
			}
			// Human-only operator guard, NOT a same-UID security boundary.
			if runtime.GOOS != "darwin" || !acceptRisk || !term.IsTerminal(int(os.Stdin.Fd())) {
				return fmt.Errorf("write requires a Mac terminal and --acknowledge-same-uid-risk; no secret stored")
			}
			raw, err := json.Marshal(credential)
			if err != nil {
				return fmt.Errorf("cannot encode credential")
			}
			defer func() {
				for i := range raw {
					raw[i] = 0
				}
			}()
			store, err := a.FileStore(cmd.Context())
			if err != nil {
				return err
			}
			// Concurrent-safe no-overwrite; do not use Resolver.Set, which overwrites.
			if err := store.SetIfAbsent(cmd.Context(), name, raw, backend.Meta{}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "one record stored in local age vault; existing entries were not overwritten")
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Owner-only Apple Passwords CSV file; never pass via stdin")
	cmd.Flags().StringVar(&site, "site", "", "Exact HTTPS hostname (e.g. example.com)")
	cmd.Flags().StringVar(&name, "name", "", "Exact web.<site> storage name")
	cmd.Flags().BoolVar(&write, "write", false, "Import one record into local encrypted age store; default is validate-only")
	cmd.Flags().BoolVar(&acceptRisk, "acknowledge-same-uid-risk", false, "Acknowledge agent processes using the same OS user could still read vault values")
	return cmd
}
