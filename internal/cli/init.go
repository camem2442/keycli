package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/spf13/cobra"
)

func newInitCmd(a *app.App) *cobra.Command {
	var printIdentity bool
	cmd := &cobra.Command{
		Use:     "init",
		Aliases: []string{"keygen"},
		Short:   "Show the public recipient (age1…) for this machine's age identity",
		Long: `Resolve the existing age identity (KC_KEY → ~/.kc/receiver.key → keychain kc.age)
and print its public recipient. A new identity is created only when none exists,
and it is written to ~/.kc/receiver.key with 0600 permissions.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			opts := file.IdentityOptions{
				Home:     a.Home,
				Config:   a.Config,
				Security: a.Deps.SecurityRunner,
			}
			res, err := file.ResolveIdentity(ctx, opts)
			generated := false
			generatedKey := ""
			switch {
			case errors.Is(err, file.ErrNoIdentity):
				generatedKey, err = file.GenerateKCKey()
				if err != nil {
					return err
				}
				id, perr := age.ParseX25519Identity(generatedKey)
				if perr != nil {
					return perr
				}
				res = file.ResolvedIdentity{Identity: id, Source: file.IdentitySourceGenerated}
				generated = true
			case err != nil:
				return err
			}

			if generated {
				path, serr := file.SaveIdentityFile(a.Home, generatedKey)
				if errors.Is(serr, os.ErrExist) {
					// Another process created receiver.key first: use it instead
					// of printing a recipient that is not stored.
					res, err = file.ResolveIdentity(ctx, opts)
					if err != nil {
						return err
					}
				} else if serr != nil {
					return fmt.Errorf("age identity 저장 실패: %w", serr)
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "kc: age identity를 %s 에 저장했습니다 (0600).\n", path)
				}
			}
			if !generated {
				fmt.Fprintf(cmd.ErrOrStderr(), "kc: 기존 age identity 사용 (%s).\n", sourceLabel(res))
			}

			out := cmd.OutOrStdout()
			if printIdentity {
				key, ierr := res.IdentityString()
				if ierr != nil {
					return ierr
				}
				fmt.Fprintln(cmd.ErrOrStderr(), "kc: --print-identity는 비밀 identity를 stdout에 출력합니다. 로그·셸 기록에 남기지 마세요.")
				fmt.Fprintln(out, key)
				return nil
			}

			rec, rerr := res.Recipient()
			if rerr != nil {
				return rerr
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "kc: 아래 공개 수신자(age1…)로 암호화해 보내세요: kc export … --to age1…")
			fmt.Fprintln(out, fmt.Sprint(rec))
			if runtime.GOOS == "darwin" {
				fmt.Fprintln(cmd.ErrOrStderr(), "kc: macOS에서는 키체인 항목 kc.age 또는 KC_KEY 환경변수도 사용할 수 있습니다.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&printIdentity, "print-identity", false,
		"print the secret age identity instead of the public recipient (for scripts; do not log)")
	return cmd
}

func sourceLabel(res file.ResolvedIdentity) string {
	switch res.Source {
	case file.IdentitySourceEnv:
		return "KC_KEY"
	case file.IdentitySourceFile:
		return res.Path
	case file.IdentitySourceKeychain:
		return "keychain kc.age"
	default:
		return string(res.Source)
	}
}
