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
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newSetCmd(a *app.App) *cobra.Command {
	var fromClip bool
	var note string
	var expiresStr string
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
				value = []byte(strings.TrimRight(text, "\n"))
			} else if !term.IsTerminal(int(os.Stdin.Fd())) {
				value, err = readAllStdin()
				if err != nil {
					return err
				}
				value = trimTrailingNewline(value)
			} else {
				fmt.Fprintf(os.Stderr, "값 입력: ")
				b, err := term.ReadPassword(int(os.Stdin.Fd()))
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
				value = b
			}
			var exp *time.Time
			if expiresStr != "" {
				t, err := time.Parse("2006-01-02", expiresStr)
				if err != nil {
					return fmt.Errorf("invalid --expires: %w", err)
				}
				exp = &t
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
	return cmd
}

func readAllStdin() ([]byte, error) {
	return io.ReadAll(os.Stdin)
}

// trimTrailingNewline removes exactly one line ending (LF or CRLF). Piped
// input almost always ends with a newline that is not part of the secret.
func trimTrailingNewline(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		b = b[:len(b)-1]
		if len(b) > 0 && b[len(b)-1] == '\r' {
			b = b[:len(b)-1]
		}
	}
	return b
}
