package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/spf13/cobra"
)

func newGitCredentialCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "git-credential",
		Short:  "Git credential helper (read-only get; store/erase no-op)",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Provide github.com credentials from kc",
		RunE: func(cmd *cobra.Command, args []string) error {
			fields, err := readCredentialInput(os.Stdin)
			if err != nil {
				return err
			}
			host := strings.ToLower(fields["host"])
			if host != "github.com" {
				return nil
			}
			val, _, err := a.Resolver.Get(readCtx(cmd, "git-credential"), "github")
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "protocol=https\nhost=github.com\nusername=git\npassword=%s\n\n", string(val))
			return nil
		},
	})
	for _, sub := range []string{"store", "erase"} {
		cmd.AddCommand(&cobra.Command{
			Use:   sub,
			Short: "Ignored (kc never writes tokens to gitconfig)",
			RunE: func(cmd *cobra.Command, args []string) error {
				_, _ = readCredentialInput(os.Stdin)
				return nil
			},
		})
	}
	return cmd
}

func readCredentialInput(r io.Reader) (map[string]string, error) {
	sc := bufio.NewScanner(r)
	fields := map[string]string{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, "=")
		if ok {
			fields[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return fields, nil
}
