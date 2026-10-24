package cli

import (
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

func newGetCmd(a *app.App) *cobra.Command {
	var noNewline bool
	var showSource bool
	cmd := &cobra.Command{
		Use:   "get NAME",
		Short: "Print secret value to stdout",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			val, _, src, err := a.Resolver.GetWithSource(cmd.Context(), args[0])
			if err != nil {
				if kcerrors.Code(err) == kcerrors.ExitNotFound {
					fmt.Fprintf(os.Stderr, "kc: %s 없음. 사용자에게 'kc set %s' 실행을 요청하세요.\n", args[0], args[0])
				}
				return err
			}
			if showSource && src != "" {
				fmt.Fprintf(os.Stderr, "kc: source: %s\n", src)
			}
			if noNewline {
				_, err = os.Stdout.Write(val)
			} else {
				_, err = fmt.Fprintln(os.Stdout, string(val))
			}
			return err
		},
	}
	cmd.Flags().BoolVarP(&noNewline, "no-newline", "n", false, "Do not append newline")
	cmd.Flags().BoolVar(&showSource, "source", false, "Print value source to stderr (never the secret)")
	return cmd
}
