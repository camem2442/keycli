package cli

import (
	"fmt"
	"os"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/clip"
	"github.com/camem2442/keycli/internal/interactive"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

func newCpCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cp [NAME]",
		Short: "Copy a secret to clipboard (auto-clear after 30s)",
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
				selected, err := interactive.PickSecret(cmd.Context(), items, "클립보드로 복사할 시크릿 선택")
				if err != nil {
					if err == interactive.ErrCanceled {
						return nil
					}
					return err
				}
				name = selected.Name
			}
			val, _, err := a.Resolver.Get(readCtx(cmd, "cp"), name)
			if err != nil {
				if kcerrors.Code(err) == kcerrors.ExitDenied {
					fmt.Fprintf(os.Stderr, "kc: 정책에 의해 '%s' 읽기가 거부되었습니다. 사용자에게 허용 목록 변경을 요청하세요.\n", name)
				}
				return err
			}
			text := string(val)
			if err := clipboardCopyWithAutoClear(a, text, os.Getenv("KC_CLIP_OSC52") == "1"); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "kc: %s 복사됨 (%v 후 자동 삭제)\n", name, clip.AutoClearDelay())
			return nil
		},
	}
	return cmd
}
