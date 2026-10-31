package cli

import (
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/cursor"
	"github.com/camem2442/keycli/internal/opencode"
	"github.com/spf13/cobra"
)

func newWireCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wire TARGET",
		Short: "Wire local tool config (e.g. opencode-vertex, cursor-vertex)",
	}
	cmd.AddCommand(newWireOpencodeVertexCmd(a))
	cmd.AddCommand(newWireCursorVertexCmd(a))
	return cmd
}

func newWireOpencodeVertexCmd(a *app.App) *cobra.Command {
	var dryRun bool
	var configPath string
	cmd := &cobra.Command{
		Use:     "opencode-vertex",
		Aliases: []string{"opencode"},
		Short:   "Merge vertex-byok OpenAI-compatible provider into opencode.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			home := a.Home
			out, path, err := opencode.WireOpencodeVertex(home, configPath, dryRun)
			if err != nil {
				return err
			}
			if dryRun {
				_, _ = os.Stdout.Write(out)
				return nil
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "kc: wrote %s (provider %s, apiKey {env:%s})\n",
				path, opencode.ProviderID, opencode.EnvAPIKey)
			fmt.Fprintf(cmd.ErrOrStderr(), "kc: kc set vertex-byok-proxy (same as config.env PROXY_API_KEY); run:\n")
			fmt.Fprintf(cmd.ErrOrStderr(), "kc:   kc exec --only vertex-byok-proxy,PROXY_API_KEY -- opencode\n")
			fmt.Fprintf(cmd.ErrOrStderr(), "kc: ensure vertex-byok-proxy is up: cursor-vertex-status\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print merged JSON only")
	cmd.Flags().StringVar(&configPath, "config", "", "opencode.json path (default ~/.config/opencode/opencode.json)")
	return cmd
}

func newWireCursorVertexCmd(a *app.App) *cobra.Command {
	var dryRun bool
	var dbPath string
	var baseURL string
	var restart bool
	cmd := &cobra.Command{
		Use:     "cursor-vertex",
		Aliases: []string{"cursor"},
		Short:   "Update Cursor's Override OpenAI Base URL from active vertex-byok tunnel",
		RunE: func(cmd *cobra.Command, args []string) error {
			home := a.Home
			res, err := cursor.WireCursorVertex(home, dbPath, baseURL, dryRun, restart)
			if err != nil {
				return err
			}
			if dryRun {
				fmt.Fprintf(cmd.ErrOrStderr(), "kc: [dry-run] Cursor configuration preview:\n")
				fmt.Fprintf(cmd.ErrOrStderr(), "kc:   database:        %s\n", res.DBPath)
				fmt.Fprintf(cmd.ErrOrStderr(), "kc:   previous URL:    %s\n", res.PreviousURL)
				fmt.Fprintf(cmd.ErrOrStderr(), "kc:   new base URL:    %s\n", res.NewURL)
				fmt.Fprintf(cmd.ErrOrStderr(), "kc:   useOpenAIKey:    true\n")
				if res.IsRunning {
					fmt.Fprintf(cmd.ErrOrStderr(), "kc:   Cursor status:   running (PID %d)\n", res.RunningPID)
				}
				return nil
			}
			if res.Restarted {
				fmt.Fprintf(cmd.ErrOrStderr(), "kc: gracefully stopped Cursor (PID was %d), updated state DB, and relaunched Cursor.\n", res.RunningPID)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "kc: updated Cursor state DB at %s\n", res.DBPath)
			}
			if res.PreviousURL != "" && res.PreviousURL != res.NewURL {
				fmt.Fprintf(cmd.ErrOrStderr(), "kc:   previous URL: %s\n", res.PreviousURL)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "kc:   openAIBaseUrl: %s\n", res.NewURL)
			fmt.Fprintf(cmd.ErrOrStderr(), "kc:   useOpenAIKey:  true\n")

			if res.IsRunning && !res.Restarted {
				fmt.Fprintf(cmd.ErrOrStderr(), "kc: warning: Cursor is currently running (PID %d). Because Cursor caches settings in RAM, you must reload window (Cmd+Shift+P -> Developer: Reload Window) or use 'kc wire cursor-vertex --restart' to take effect.\n", res.RunningPID)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "display planned SQLite update without modifying database")
	cmd.Flags().StringVar(&dbPath, "db", "", "path to Cursor state.vscdb (default: ~/Library/Application Support/Cursor/User/globalStorage/state.vscdb)")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "explicit OpenAI Base URL override (default: auto-detected from active tunnel)")
	cmd.Flags().BoolVar(&restart, "restart", false, "gracefully quit running Cursor before updating DB and relaunch it")
	return cmd
}
