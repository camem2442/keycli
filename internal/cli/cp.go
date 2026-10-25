package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/clip"
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
				var err error
				name, err = fuzzyPickName(a, cmd)
				if err != nil {
					return err
				}
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

func fuzzyPickName(a *app.App, cmd *cobra.Command) (string, error) {
	items, err := a.Resolver.List(cmd.Context())
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", kcerrors.ErrNotFound
	}
	if !isTerminal(os.Stdin) {
		return "", kcerrors.ErrUsage
	}
	fmt.Fprintln(os.Stderr, "이름:")
	for _, it := range items {
		fmt.Fprintf(os.Stderr, "  %s\n", it.Name)
	}
	fmt.Fprint(os.Stderr, "> ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", err
	}
	q := strings.TrimSpace(strings.ToLower(line))
	for _, it := range items {
		if strings.ToLower(it.Name) == q || strings.Contains(strings.ToLower(it.Name), q) {
			return it.Name, nil
		}
	}
	return "", kcerrors.ErrNotFound
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
