package cli

import (
	"fmt"
	"os"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/agebundle"
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/dotenv"
	"github.com/camem2442/keycli/internal/exportrecv"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/transferguard"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newExportCmd(a *app.App) *cobra.Command {
	var format string
	var toRecipient string
	var toGitHub string
	var stdout bool
	var verifiedRecipient string

	cmd := &cobra.Command{
		Use:   "export NAME...",
		Short: "Export secrets (dotenv clipboard or age bundle)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("verified-recipient") {
				if format != "" || toGitHub != "" || toRecipient == "" || verifiedRecipient == "" || len(args) != 1 {
					return fmt.Errorf("verified export requires one secret and exactly one explicit age recipient")
				}
				if err := transferguard.CheckRecipient(toRecipient, verifiedRecipient); err != nil {
					return err
				}
				parsed, ssh, err := exportrecv.ParseRecipient(toRecipient)
				if err != nil {
					return err
				}
				if _, ok := parsed.(*age.X25519Recipient); !ok || ssh {
					return fmt.Errorf("verified export requires an X25519 recipient")
				}
			}
			secrets := map[string][]byte{}
			for _, name := range args {
				val, _, err := a.Resolver.Get(readCtx(cmd, "export"), name)
				if err != nil {
					if kcerrors.Code(err) == kcerrors.ExitNotFound {
						fmt.Fprintf(cmd.ErrOrStderr(), "kc: %s 없음. 사용자에게 'kc set %s' 실행을 요청하세요.\n", name, name)
					}
					if kcerrors.Code(err) == kcerrors.ExitDenied {
						fmt.Fprintf(cmd.ErrOrStderr(), "kc: 정책에 의해 '%s' 읽기가 거부되었습니다. 사용자에게 허용 목록 변경을 요청하세요.\n", name)
					}
					return err
				}
				secrets[a.Resolver.CanonicalName(name)] = val
			}
			if format == "dotenv" {
				return exportDotenv(cmd, a, secrets, stdout)
			}
			if toRecipient != "" || toGitHub != "" {
				return exportAge(cmd, secrets, toRecipient, toGitHub)
			}
			return kcerrors.ErrUsage
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "dotenv: copy to clipboard (never print to TTY)")
	cmd.Flags().StringVar(&toRecipient, "to", "", "age1… or ssh-… recipient")
	cmd.Flags().StringVar(&toGitHub, "to-github", "", "Fetch ssh keys from github.com/USER.keys")
	cmd.Flags().BoolVar(&stdout, "stdout", false, "Write dotenv to stdout when stdout is not a TTY")
	cmd.Flags().StringVar(&verifiedRecipient, "verified-recipient", "", "recipient independently verified by operator before reading secrets")
	return cmd
}

func exportDotenv(cmd *cobra.Command, a *app.App, secrets map[string][]byte, stdout bool) error {
	text := dotenv.Format(secrets)
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	if !isTTY && stdout {
		_, err := os.Stdout.WriteString(text)
		return err
	}
	if isTTY && stdout {
		fmt.Fprintln(cmd.ErrOrStderr(), "kc: --stdout는 stdout이 TTY가 아닐 때만 사용하세요. 클립보드로 복사합니다.")
	}
	if err := clipboardCopyWithAutoClear(a, text, true); err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "kc: dotenv를 클립보드에 복사했습니다. 자동 삭제됩니다.")
	return nil
}

func exportAge(cmd *cobra.Command, secrets map[string][]byte, toRecipient, toGitHub string) error {
	var recipients []age.Recipient
	var sshUsed bool
	if toRecipient != "" {
		r, ssh, err := exportrecv.ParseRecipient(toRecipient)
		if err != nil {
			return err
		}
		recipients = append(recipients, r)
		if ssh {
			sshUsed = true
		}
	}
	if toGitHub != "" {
		rs, err := exportrecv.FetchGitHubKeys(toGitHub, nil)
		if err != nil {
			return err
		}
		recipients = append(recipients, rs...)
		sshUsed = true
	}
	if sshUsed {
		fmt.Fprintln(cmd.ErrOrStderr(), "kc: 경고: SSH 수신자를 쓰면 암호문에 공개키 태그가 남습니다.")
	}
	if err := agebundle.Encrypt(os.Stdout, secrets, recipients); err != nil {
		return err
	}
	return nil
}
