package cli

import (
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd(a *app.App) *cobra.Command {
	var allowSecretValues bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run keycli as a Model Context Protocol (MCP) stdio server",
		Long: `Runs a standard MCP (Model Context Protocol) JSON-RPC 2.0 server over stdio.
Metadata and diagnostics only by default. To expose get_secret responses to
trusted MCP clients, explicitly set --allow-secret-values. Secret values may
appear in the client or model context; kc exec --require is safer for execution.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := mcp.NewServer(a, os.Stdin, os.Stdout, mcp.Options{AllowSecretValues: allowSecretValues})
			return srv.Run(cmd.Context())
		},
	}
	cmd.Flags().BoolVar(&allowSecretValues, "allow-secret-values", false, "Opt in to exposing get_secret values to the MCP client")
	return cmd
}
