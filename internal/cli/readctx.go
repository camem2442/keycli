package cli

import (
	"context"

	"github.com/camem2442/keycli/internal/middleware"
	"github.com/spf13/cobra"
)

func readCtx(cmd *cobra.Command, command string) context.Context {
	return middleware.WithCommand(cmd.Context(), command)
}
