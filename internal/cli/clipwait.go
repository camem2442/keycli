package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/clip"
	"github.com/spf13/cobra"
)

func newClipWaitClearCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "_clip-wait-clear",
		Hidden: true,
		Short:  "Wait and clear clipboard if unchanged (internal)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// The checksum travels over stdin so it never appears in argv.
			raw, err := io.ReadAll(io.LimitReader(os.Stdin, 256))
			if err != nil {
				return nil
			}
			checksum := strings.TrimSpace(string(raw))
			if checksum == "" {
				return nil
			}
			time.Sleep(clip.AutoClearDelay())
			cp := clip.Provider{Config: a.Config, OSC52Enabled: os.Getenv("KC_CLIP_OSC52") == "1"}
			cur, err := cp.Read()
			if err != nil {
				return nil
			}
			if clip.Checksum(cur) == checksum {
				_ = cp.Clear()
			}
			return nil
		},
	}
	return cmd
}

// clipboardCopyWithAutoClear writes text and schedules a clear after the
// auto-clear delay. Unlike clip.Provider.CopyWithAutoClear, the wait happens in
// a detached child process so it survives the kc process exiting (export).
func clipboardCopyWithAutoClear(a *app.App, text string, osc52 bool) error {
	delay := clip.AutoClearDelay()
	cp := clip.Provider{Config: a.Config, OSC52Enabled: osc52}
	sum := clip.Checksum(text)
	if err := cp.Write(text); err != nil {
		return err
	}
	if os.Getenv("KC_CP_WAIT_CLEAR") == "1" {
		time.Sleep(delay + 50*time.Millisecond)
		cur, err := cp.Read()
		if err == nil && clip.Checksum(cur) == sum {
			_ = cp.Clear()
		}
		return nil
	}
	// Pass the checksum over a pipe instead of argv: the checksum of a secret
	// is sensitive (it allows offline guesses and equality checks).
	pr, pw, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kc: 경고: 클립보드 자동 삭제 예약 실패: %v\n", err)
		return nil
	}
	clearCmd := exec.Command(os.Args[0], "_clip-wait-clear")
	clearCmd.Env = os.Environ()
	clearCmd.Stdin = pr
	clearCmd.Stdout = nil
	clearCmd.Stderr = nil
	if err := clearCmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		fmt.Fprintf(os.Stderr, "kc: 경고: 클립보드 자동 삭제 예약 실패: %v\n", err)
		return nil
	}
	_, writeErr := io.WriteString(pw, sum+"\n")
	_ = pw.Close()
	_ = pr.Close()
	if writeErr != nil {
		fmt.Fprintf(os.Stderr, "kc: 경고: 클립보드 자동 삭제 예약 실패: %v\n", writeErr)
	}
	return nil
}
