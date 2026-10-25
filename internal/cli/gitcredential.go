package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/spf13/cobra"
)

// credentialCommandTimeout bounds a user-configured command so a hung helper
// cannot block git forever.
const credentialCommandTimeout = 60 * time.Second

func newGitCredentialCmd(a *app.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "git-credential",
		Short:  "Git credential helper (read-only get; store/erase no-op)",
		Hidden: true,
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Provide credentials for hosts mapped in config [git.<host>]",
		RunE: func(cmd *cobra.Command, args []string) error {
			fields, err := readCredentialInput(os.Stdin)
			if err != nil {
				return err
			}
			host := strings.TrimSpace(fields["host"])
			spec, ok := a.Config.GitHost(host)
			if !ok {
				return nil
			}
			name := strings.TrimSpace(spec.Secret)
			if name == "" {
				name = host
			}
			val, _, err := a.Resolver.Get(readCtx(cmd, "git-credential"), name)
			if err != nil {
				// The implicit github.com default is a convenience only: stay
				// silent when the secret does not exist so git can fall back to
				// its other helpers. Explicit mappings still fail loudly.
				if spec.Implicit && errors.Is(err, kcerrors.ErrNotFound) {
					return nil
				}
				return err
			}
			password := string(val)
			if cmdline := strings.TrimSpace(spec.Command); cmdline != "" {
				password, err = runCredentialCommand(cmd.Context(), cmdline, a.Resolver.EnvNames(name), string(val))
				if err != nil {
					return fmt.Errorf("git-credential: command for host %q failed: %w", host, err)
				}
				if password == "" {
					return fmt.Errorf("git-credential: command for host %q produced no password", host)
				}
			}
			username := strings.TrimSpace(spec.Username)
			if username == "" {
				username = "git"
			}
			protocol := strings.TrimSpace(fields["protocol"])
			if protocol == "" {
				protocol = "https"
			}
			fmt.Fprintf(os.Stdout, "protocol=%s\nhost=%s\nusername=%s\npassword=%s\n\n", protocol, host, username, password)
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

// runCredentialCommand runs a user-configured helper command with the secret
// exported through the environment (never argv) and returns its stdout as the
// password. Stderr is not forwarded so a misbehaving command cannot leak the
// secret into git's output.
func runCredentialCommand(parent context.Context, cmdline string, envNames []string, secret string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, credentialCommandTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "sh", "-c", cmdline)
	c.Env = os.Environ()
	for _, key := range envNames {
		c.Env = append(c.Env, key+"="+secret)
	}
	var stdout strings.Builder
	c.Stdout = &stdout
	c.Stderr = io.Discard
	if err := c.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
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
