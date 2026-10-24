package cli

import (
	"fmt"
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/ledger"
	"github.com/spf13/cobra"
	"os"
)

func newStatusCmd(a *app.App) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show secrets that changed since last push/export",
		RunE: func(cmd *cobra.Command, args []string) error {
			key, err := a.LedgerHMACKey(cmd.Context())
			if err != nil {
				return err
			}
			lf := a.LedgerFile()
			entries, err := lf.Load()
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "kc: 원장에 기록된보내기가 없습니다.")
				return nil
			}
			var stale []string
			for _, e := range entries {
				val, _, err := a.Resolver.Get(cmd.Context(), e.Name)
				if err != nil {
					continue
				}
				if lf.Stale(key, e.Name, e.Target, val) {
					stale = append(stale, ledger.StatusLine(e.Name, e.Target, e.ExportedAt))
				}
			}
			if len(stale) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "kc: 모든 기록된 시크릿이 마지막보내기와 일치합니다.")
				return nil
			}
			fmt.Fprintln(os.Stderr, "kc: 재전송 필요:")
			for _, line := range stale {
				fmt.Fprintln(os.Stderr, "  "+line)
			}
			return nil
		},
	}
}
