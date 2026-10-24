package cli

import (
	"fmt"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

func newLinkCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link NAME keychain:SERVICE[/ACCOUNT]",
		Short: "Link an existing keychain item (stores reference only)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := a.Resolver.CanonicalName(args[0])
			tgt, err := config.ParseLinkURI(args[1])
			if err != nil {
				return err
			}
			if a.Config.Link == nil {
				a.Config.Link = map[string]config.LinkTarget{}
			}
			a.Config.Link[name] = tgt
			config.EnsureLinkBackend(&a.Config)
			if err := a.SaveConfig(); err != nil {
				return err
			}
			if err := a.Reload(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "kc: linked %s -> %s:%s", name, tgt.Service, tgt.Account)
			if tgt.Account == "" {
				fmt.Fprintf(cmd.ErrOrStderr(), " (any account)")
			}
			fmt.Fprintln(cmd.ErrOrStderr())
			fmt.Fprintln(cmd.ErrOrStderr(), "kc: 첫 읽기 때 macOS가 '항상 허용'을 물을 수 있습니다 (다른 앱이 만든 항목).")
			return nil
		},
	}
	return cmd
}

func newUnlinkCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlink NAME",
		Short: "Remove a keychain link from config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := a.Resolver.CanonicalName(args[0])
			key := ""
			for k := range a.Config.Link {
				if strings.EqualFold(k, name) {
					key = k
					break
				}
			}
			if key == "" {
				return fmt.Errorf("%w: %s", kcerrors.ErrNotFound, name)
			}
			delete(a.Config.Link, key)
			if err := a.SaveConfig(); err != nil {
				return err
			}
			return a.Reload(cmd.Context())
		},
	}
	return cmd
}
