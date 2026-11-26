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
	"github.com/camem2442/keycli/internal/totp"
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
	var normalize bool
	var kind, url, username string
	cmd := &cobra.Command{
		Use:   "set [NAME]",
		Short: "Store a secret (stdin, prompt, --clip, or interactive wizard)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 && (kind != "" || url != "" || username != "") {
				return fmt.Errorf("--kind, --url and --username need an explicit NAME")
			}
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
			// Validate the metadata flags before reading a value or touching the
			// clipboard, so a typo cannot consume a pasted secret.
			if kind != "" && !validSecretKind(kind) {
				return fmt.Errorf("invalid --kind %q; use %s", kind, strings.Join(secretKinds, "|"))
			}
			if (url != "" || username != "") && kind != "login" {
				return fmt.Errorf("--url and --username require --kind login")
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
			if normalize {
				value = secret.NormalizeSecret(value)
				if len(value) == 0 {
					return kcerrors.ErrUsage
				}
			}
			// The seed itself is validated here; Parse errors never repeat it.
			if kind == "otp" {
				if _, err := totp.Parse(string(value)); err != nil {
					fmt.Fprintf(os.Stderr, "kc: 경고: %v — 저장은 계속되지만 'kc otp %s'는 실패합니다.\n", err, name)
				}
			}
			var exp *time.Time
			if expiresStr != "" {
				t, err := time.Parse("2006-01-02", expiresStr)
				if err != nil {
					return fmt.Errorf("invalid --expires: %w", err)
				}
				exp = &t
			}
			// A seed is not a provider token: probing would send it to the
			// provider API. kc otp validates it locally instead.
			if !noVerify && kind != "otp" {
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
				Note:     note,
				Expires:  exp,
				Kind:     kind,
				URL:      url,
				Username: username,
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
	cmd.Flags().BoolVar(&normalize, "normalize", false, "Trim surrounding spaces and remove all CR/LF (for pasted single-line PATs)")
	cmd.Flags().StringVar(&kind, "kind", "", "Secret kind: "+strings.Join(secretKinds, "|"))
	cmd.Flags().StringVar(&url, "url", "", "With --kind login: website or app URL (file store only)")
	cmd.Flags().StringVar(&username, "username", "", "With --kind login: account name (file store only)")
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

// secretKinds are the recognised secret kinds. Empty means unclassified, which
// is what every entry stored before kinds existed reports.
var secretKinds = []string{"login", "token", "key", "otp", "note"}

func validSecretKind(kind string) bool {
	for _, k := range secretKinds {
		if kind == k {
			return true
		}
	}
	return false
}
