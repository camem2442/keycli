package cli

import (
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run keycli as a Model Context Protocol (MCP) stdio server",
		Long: `Runs a standard MCP (Model Context Protocol) JSON-RPC 2.0 server over stdio.
Enables AI tools like Claude Desktop, Cursor, and OpenCode to query secrets,
list keys, and check policies directly via native tool calls.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := mcp.NewServer(a, os.Stdin, os.Stdout)
			return srv.Run(cmd.Context())
		},
	}
}
