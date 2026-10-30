package cli

import (
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/interactive"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

func newRmCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "rm [NAME]",
		Short: "Delete a secret",
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
				selected, err := interactive.PickSecret(cmd.Context(), items, "삭제할 시크릿 선택")
				if err != nil {
					if err == interactive.ErrCanceled {
						return nil
					}
					return err
				}
				confirmed, err := interactive.PromptConfirm(fmt.Sprintf("정말로 시크릿 '%s'을(를) 삭제하시겠습니까?", selected.Name), false)
				if err != nil || !confirmed {
					return nil
				}
				name = selected.Name
			}
			return a.Resolver.Delete(cmd.Context(), name)
		},
	}
}
