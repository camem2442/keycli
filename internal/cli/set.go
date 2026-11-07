package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/clip"
	"github.com/camem2442/keycli/internal/interactive"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/secret"
	"github.com/camem2442/keycli/internal/verify"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newSetCmd(a *app.App) *cobra.Command {
	var fromClip bool
	var note string
	var expiresStr string
	var noVerify bool
	var force bool
	cmd := &cobra.Command{
		Use:   "set [NAME]",
		Short: "Store a secret (stdin, prompt, --clip, or interactive wizard)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 0 {
				if !term.IsTerminal(int(os.Stdin.Fd())) && !fromClip {
					return kcerrors.ErrUsage
				}
				if !fromClip && note == "" && expiresStr == "" {
					_, err := interactive.RunSetWizard(cmd.Context(), a, "")
					return err
				}
				promptedName, err := interactive.PromptLine("시크릿 이름", "")
				if err != nil {
					return err
				}
				name = strings.TrimSpace(promptedName)
				if name == "" {
					return fmt.Errorf("시크릿 이름을 입력해주세요")
				}
			} else {
				name = args[0]
			}

			var value []byte
			var err error
			var cp clip.Provider
			if fromClip {
				cp = clip.Provider{Config: a.Config}
				text, err := cp.Read()
				if err != nil {
					return err
				}
				value = []byte(text)
			} else if !term.IsTerminal(int(os.Stdin.Fd())) {
				value, err = readAllStdin()
				if err != nil {
					return err
				}
			} else {
				fmt.Fprintf(os.Stderr, "값 입력: ")
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
				value = b
			}
			value = secret.NormalizeSecret(value)
			if len(value) == 0 {
				return kcerrors.ErrUsage
			}
			var exp *time.Time
			if expiresStr != "" {
				t, err := time.Parse("2006-01-02", expiresStr)
				if err != nil {
					return fmt.Errorf("invalid --expires: %w", err)
				}
				exp = &t
			}
			if !noVerify {
				vout, err := verify.Run(cmd.Context(), name, note, value, nil)
				if err != nil {
					return err
				}
				if vout != nil {
					vout.Print(os.Stdout)
					if vout.Block && !force {
						return verify.ErrVerifyFailed
					}
					if vout.Warn {
						fmt.Fprintln(os.Stderr, "kc: set-time verify: 일부 프로브 경고(저장은 계속됩니다)")
					}
				}
			}
			if err := a.Resolver.Set(cmd.Context(), name, value, backend.Meta{
				Note:    note,
				Expires: exp,
			}); err != nil {
				return err
			}
			// Clear the clipboard only after the value was stored, so a failed
			// save does not destroy the user's only copy.
			if fromClip {
				_ = cp.Clear()
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fromClip, "clip", false, "Read from clipboard, then clear clipboard")
	cmd.Flags().StringVar(&note, "note", "", "Note metadata")
	cmd.Flags().StringVar(&expiresStr, "expires", "", "Expiry date YYYY-MM-DD")
	cmd.Flags().BoolVar(&noVerify, "no-verify", false, "Skip set-time provider verification")
	cmd.Flags().BoolVar(&force, "force", false, "Store even when set-time verify fails")
	return cmd
}

func readAllStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin)
}
