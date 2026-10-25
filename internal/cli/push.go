package cli

import (
	"fmt"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/ledger"
	"github.com/camem2442/keycli/internal/target"
	"github.com/camem2442/keycli/internal/target/cursor"
	"github.com/camem2442/keycli/internal/target/gh"
	"github.com/spf13/cobra"
)

func newPushCmd(a *app.App) *cobra.Command {
	var cursorEnvID string
	var repos []string
	var secretType string
	var dryRun bool
	var ghUser bool
	var ghApp string

	cmd := &cobra.Command{
		Use:   "push <cursor|gh> NAME...",
		Short: "Push secrets to a remote platform",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetName := args[0]
			names := args[1:]
			var secrets []target.Secret
			for _, name := range names {
				val, _, err := a.Resolver.Get(readCtx(cmd, "push"), name)
				if err != nil {
					if kcerrors.Code(err) == kcerrors.ExitNotFound {
						fmt.Fprintf(cmd.ErrOrStderr(), "kc: %s 없음. 사용자에게 'kc set %s' 실행을 요청하세요.\n", name, name)
					}
					if kcerrors.Code(err) == kcerrors.ExitDenied {
						fmt.Fprintf(cmd.ErrOrStderr(), "kc: 정책에 의해 '%s' 읽기가 거부되었습니다. 사용자에게 허용 목록 변경을 요청하세요.\n", name)
					}
					return err
				}
				canon := a.Resolver.CanonicalName(name)
				secrets = append(secrets, target.Secret{Name: canon, Value: val})
			}
			cursorOpts := cursor.Options{
				EnvironmentID: cursorEnvID,
				Repos:         repos,
				SecretType:    secretType,
				DryRun:        dryRun,
			}
			ghOpts := gh.Options{
				User:   ghUser,
				Repos:  repos,
				App:    ghApp,
				DryRun: dryRun,
			}
			switch targetName {
			case "cursor":
				if err := cursor.Push(cmd.Context(), cursorOpts, a.Resolver, a.Config, secrets); err != nil {
					return err
				}
			case "gh":
				if err := gh.Push(cmd.Context(), ghOpts, a.Config, secrets); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown push target %q (available: cursor, gh)", targetName)
			}
			if dryRun {
				return nil
			}
			key, err := a.LedgerHMACKey(cmd.Context())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "kc: 원장 기록 생략 (파일 저장소 잠김): %v\n", err)
				return nil
			}
			lf := a.LedgerFile()
			items := make([]ledger.RecordItem, 0, len(secrets))
			for _, sec := range secrets {
				items = append(items, ledger.RecordItem{Name: sec.Name, Target: targetName, Value: sec.Value})
			}
			if err := lf.RecordAll(key, items); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cursorEnvID, "env-id", "", "Cursor environment ID (or KC_CURSOR_ENVIRONMENT_ID)")
	cmd.Flags().StringSliceVar(&repos, "repos", nil, "Repository scope (cursor / gh)")
	cmd.Flags().StringVar(&secretType, "type", "runtime_secret", "Cursor secret type: runtime_secret|environment_variable")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print actions without calling remote APIs")
	cmd.Flags().BoolVar(&ghUser, "user", false, "gh: set user secret")
	cmd.Flags().StringVar(&ghApp, "app", "codespaces", "gh: codespaces|actions|agents")
	return cmd
}
