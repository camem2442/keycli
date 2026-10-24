package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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
			sum := clip.Checksum(text)
			cp := clip.Provider{Config: a.Config, OSC52Enabled: os.Getenv("KC_CLIP_OSC52") == "1"}
			if err := cp.Write(text); err != nil {
				return err
			}
			delay := clip.AutoClearDelay()
			if os.Getenv("KC_CP_WAIT_CLEAR") == "1" {
				time.Sleep(delay + 50*time.Millisecond)
				cur, err := cp.Read()
				if err == nil && clip.Checksum(cur) == sum {
					_ = cp.Clear()
				}
			} else {
				clearCmd := exec.Command(os.Args[0], "_clip-wait-clear", "--checksum", sum)
				clearCmd.Env = os.Environ()
				clearCmd.Stdout = nil
				clearCmd.Stderr = nil
				_ = clearCmd.Start()
			}
			fmt.Fprintf(os.Stderr, "kc: %s 복사됨 (%v 후 자동 삭제)\n", name, delay)
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
