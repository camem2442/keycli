package cli

import (
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/interactive"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

func newGetCmd(a *app.App) *cobra.Command {
	var noNewline bool
	var showSource bool
	cmd := &cobra.Command{
		Use:   "get [NAME]",
		Short: "Print secret value to stdout",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			} else {
				if !interactive.IsTerminal(os.Stdin) {
					return kcerrors.ErrUsage
				}
				items, err := a.Resolver.List(cmd.Context())
				if err != nil {
					return err
				}
				if len(items) == 0 {
					return kcerrors.ErrNotFound
				}
				selected, err := interactive.PickSecret(cmd.Context(), items, "조회할 시크릿 선택")
				if err != nil {
					if err == interactive.ErrCanceled {
						return nil
					}
					return err
				}
				name = selected.Name
			}

			val, _, src, err := a.Resolver.GetWithSource(readCtx(cmd, "get"), name)
			if err != nil {
				if kcerrors.Code(err) == kcerrors.ExitNotFound {
					fmt.Fprintf(os.Stderr, "kc: %s 없음. 사용자에게 'kc set %s' 실행을 요청하세요.\n", name, name)
					if a.Resolver.HasEnvBackend() {
						fmt.Fprintln(os.Stderr, kcerrors.PhaseHint)
					}
				}
				if kcerrors.Code(err) == kcerrors.ExitDenied {
					fmt.Fprintf(os.Stderr, "kc: 정책에 의해 '%s' 읽기가 거부되었습니다. 사용자에게 허용 목록 변경을 요청하세요.\n", name)
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
