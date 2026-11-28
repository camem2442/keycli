package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/agebundle"
	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/applecsv"
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
	var outputFile string
	var allowPlaintext bool

	cmd := &cobra.Command{
		Use:   "export NAME...",
		Short: "Export secrets (dotenv clipboard, age bundle, or Apple Passwords CSV)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// --format csv uses --output-file too, but it is not an age bundle.
			guarded := format != "csv" &&
				(cmd.Flags().Changed("verified-recipient") || cmd.Flags().Changed("output-file"))
			if guarded {
				if format != "" || toGitHub != "" || stdout || toRecipient == "" || verifiedRecipient == "" || outputFile == "" || len(args) != 1 {
					return fmt.Errorf("verified export requires one secret, one explicit age recipient, verified recipient and output file")
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
			switch format {
			case "", "dotenv":
			case "csv":
				if !allowPlaintext {
					return fmt.Errorf("csv export writes plaintext; re-run with --allow-plaintext")
				}
				if outputFile == "" {
					return fmt.Errorf("csv export requires --output-file")
				}
				if stdout || cmd.Flags().Changed("to") || cmd.Flags().Changed("to-github") ||
					cmd.Flags().Changed("verified-recipient") {
					return fmt.Errorf("csv export cannot be combined with --stdout, --to, --to-github or --verified-recipient")
				}
				return exportAppleCSV(cmd, a, args, outputFile)
			default:
				return fmt.Errorf("unknown --format %q; use dotenv or csv", format)
			}
			if allowPlaintext {
				return fmt.Errorf("--allow-plaintext applies only to --format csv")
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
				if guarded {
					f, err := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
					if err != nil {
						return fmt.Errorf("create encrypted output: %w", err)
					}
					err = exportAge(cmd, f, secrets, toRecipient, toGitHub)
					closeErr := f.Close()
					if err != nil {
						return fmt.Errorf("export failed; partial encrypted file retained: %w", err)
					}
					if closeErr != nil {
						return fmt.Errorf("close encrypted file; inspect file before reuse: %w", closeErr)
					}
					return nil
				}
				return exportAge(cmd, os.Stdout, secrets, toRecipient, toGitHub)
			}
			return kcerrors.ErrUsage
		},
	}
	cmd.Flags().StringVar(&format, "format", "", "dotenv: copy to clipboard (never print to TTY); csv: plaintext file for Apple Passwords")
	cmd.Flags().StringVar(&toRecipient, "to", "", "age1… or ssh-… recipient")
	cmd.Flags().StringVar(&toGitHub, "to-github", "", "Fetch ssh keys from github.com/USER.keys")
	cmd.Flags().BoolVar(&stdout, "stdout", false, "Write dotenv to stdout when stdout is not a TTY")
	cmd.Flags().BoolVar(&allowPlaintext, "allow-plaintext", false, "With --format csv: acknowledge that the output file is unencrypted")
	cmd.Flags().StringVar(&verifiedRecipient, "verified-recipient", "", "recipient independently verified by operator before reading secrets")
	cmd.Flags().StringVar(&outputFile, "output-file", "", "create a new encrypted bundle file (0600; requires verified recipient); with --format csv: plaintext CSV file (0600)")
	return cmd
}

// exportAppleCSV writes the six-column CSV that the Apple Passwords app
// imports. Notes come from kc metadata; URL and OTPAuth stay empty because kc
// does not hold them.
func exportAppleCSV(cmd *cobra.Command, a *app.App, names []string, outputFile string) error {
	items, err := a.Resolver.List(cmd.Context())
	if err != nil {
		return err
	}
	notes := make(map[string]string, len(items))
	for _, it := range items {
		notes[strings.ToLower(it.Name)] = it.Note
	}

	rows := make([]applecsv.Row, 0, len(names))
	for _, name := range names {
		canon := a.Resolver.CanonicalName(name)
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
		if !utf8.Valid(val) {
			return fmt.Errorf("secret %s is not valid UTF-8; the csv format is text-only", canon)
		}
		rows = append(rows, applecsv.Row{
			Title:    canon,
			Password: string(val),
			Notes:    notes[strings.ToLower(canon)],
		})
	}

	data, err := applecsv.Format(rows)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(outputFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create csv output: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(outputFile)
		return fmt.Errorf("write csv output: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(outputFile)
		return fmt.Errorf("close csv output; the partial file was removed: %w", err)
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"kc: wrote %d row(s) to %s (plaintext, 0600). Import it in Passwords (File > Import Passwords from File), then delete it.\n",
		len(rows), outputFile)
	return nil
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

func exportAge(cmd *cobra.Command, output io.Writer, secrets map[string][]byte, toRecipient, toGitHub string) error {
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
	if err := agebundle.Encrypt(output, secrets, recipients); err != nil {
		return err
	}
	return nil
}
