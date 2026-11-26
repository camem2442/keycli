package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/middleware"
	"github.com/camem2442/keycli/internal/totp"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newOtpCmd(a *app.App) *cobra.Command {
	var allowStdout bool
	var at string
	cmd := &cobra.Command{
		Use:   "otp NAME",
		Short: "Print the current code for a stored otpauth entry",
		Long: "Derive the current time-based code from a stored otpauth URI or Base32 seed.\n" +
			"The seed is only read through the normal policy and audit path; only the code\n" +
			"is printed. Printing needs a terminal, or --stdout when stdout is a pipe.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			value, _, err := a.Resolver.Get(readCtx(cmd, "otp"), name)
			if err != nil {
				if kcerrors.Code(err) == kcerrors.ExitNotFound {
					fmt.Fprintf(cmd.ErrOrStderr(), "kc: %s 없음. 사용자에게 'kc set %s' 실행을 요청하세요.\n", name, name)
				}
				if kcerrors.Code(err) == kcerrors.ExitDenied {
					fmt.Fprintf(cmd.ErrOrStderr(), "kc: 정책에 의해 '%s' 읽기가 거부되었습니다. 사용자에게 허용 목록 변경을 요청하세요.\n", name)
				}
				return err
			}
			cfg, err := totp.Parse(string(value))
			if err != nil {
				// totp errors never repeat the seed; name the entry instead.
				return fmt.Errorf("%s: %w", name, err)
			}
			now := time.Now()
			if at != "" {
				parsed, err := time.Parse(time.RFC3339, at)
				if err != nil {
					return fmt.Errorf("invalid --at: %w", err)
				}
				now = parsed
			}
			if !allowStdout && !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("refusing to print a one-time code to a pipe; use --stdout or 'kc cp --otp %s'", name)
			}
			code, err := cfg.Code(now)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&allowStdout, "stdout", false, "Print the code even when stdout is not a terminal")
	cmd.Flags().StringVar(&at, "at", "", "Compute the code for this RFC 3339 instant instead of now")
	return cmd
}

// otpCode reads one entry through the named command's policy/audit path and
// returns its current code. Errors never carry the seed; callers print only
// the code.
func otpCode(ctx context.Context, a *app.App, command, name string, now time.Time) (totp.Config, string, error) {
	value, _, err := a.Resolver.Get(middleware.WithCommand(ctx, command), name)
	if err != nil {
		return totp.Config{}, "", err
	}
	cfg, err := totp.Parse(string(value))
	if err != nil {
		return totp.Config{}, "", fmt.Errorf("%s: %w", name, err)
	}
	code, err := cfg.Code(now)
	if err != nil {
		return totp.Config{}, "", err
	}
	return cfg, code, nil
}

// otpEnvKey derives a valid environment variable name for a generated code.
func otpEnvKey(name string) string {
	var b []byte
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
			b = append(b, c-'a'+'A')
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b = append(b, c)
		default:
			if len(b) > 0 && b[len(b)-1] != '_' {
				b = append(b, '_')
			}
		}
	}
	key := string(b)
	for len(key) > 0 && key[0] >= '0' && key[0] <= '9' {
		key = key[1:]
	}
	if key == "" {
		key = "OTP"
	}
	if strings.HasSuffix(key, "_OTP") {
		return key
	}
	return key + "_OTP"
}
