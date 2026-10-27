package cli

import (
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/opencode"
	"github.com/spf13/cobra"
)

func newWireCmd(a *app.App) *cobra.Command {
	var dryRun bool
	var configPath string
	cmd := &cobra.Command{
		Use:   "wire TARGET",
		Short: "Wire local tool config (e.g. opencode-vertex)",
	}
	cmd.AddCommand(newWireOpencodeVertexCmd(a, &dryRun, &configPath))
	return cmd
}

func newWireOpencodeVertexCmd(a *app.App, dryRun *bool, configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "opencode-vertex",
		Aliases: []string{"opencode"},
		Short:   "Merge vertex-byok OpenAI-compatible provider into opencode.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			home := a.Home
			out, path, err := opencode.WireOpencodeVertex(home, *configPath, *dryRun)
			if err != nil {
				return err
			}
			if *dryRun {
				_, _ = os.Stdout.Write(out)
				return nil
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "kc: wrote %s (provider %s, apiKey {env:%s})\n",
				path, opencode.ProviderID, opencode.EnvAPIKey)
			fmt.Fprintf(cmd.ErrOrStderr(), "kc: kc set vertex-byok-proxy (same as config.env PROXY_API_KEY); run:\n")
			fmt.Fprintf(cmd.ErrOrStderr(), "kc:   kc exec --only vertex-byok-proxy,PROXY_API_KEY -- opencode run …\n")
			fmt.Fprintf(cmd.ErrOrStderr(), "kc:   (non-standalone: exec syncs %s into `opencode service` before run)\n", opencode.EnvAPIKey)
			fmt.Fprintf(cmd.ErrOrStderr(), "kc: ensure vertex-byok-proxy is up: cursor-vertex-status\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(dryRun, "dry-run", false, "print merged JSON only")
	cmd.Flags().StringVar(configPath, "config", "", "opencode.json path (default ~/.config/opencode/opencode.json)")
	return cmd
}
