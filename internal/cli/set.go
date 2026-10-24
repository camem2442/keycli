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
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newSetCmd(a *app.App) *cobra.Command {
	var fromClip bool
	var note string
	var expiresStr string
	cmd := &cobra.Command{
		Use:   "set NAME",
		Short: "Store a secret (stdin, prompt, or --clip)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var value []byte
			var err error
			if fromClip {
				cp := clip.Provider{}
				text, err := cp.Read()
				if err != nil {
					return err
				}
				value = []byte(strings.TrimRight(text, "\n"))
				defer func() { _ = cp.Clear() }()
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
			var exp *time.Time
			if expiresStr != "" {
				t, err := time.Parse("2006-01-02", expiresStr)
				if err != nil {
					return fmt.Errorf("invalid --expires: %w", err)
				}
				exp = &t
			}
			return a.Resolver.Set(cmd.Context(), args[0], value, backend.Meta{
				Note:    note,
				Expires: exp,
			})
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
