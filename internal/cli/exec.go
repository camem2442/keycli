package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/opencode"
	"github.com/spf13/cobra"
)

func newExecCmd(a *app.App) *cobra.Command {
	var only []string
	cmd := &cobra.Command{
		Use:   "exec [--only a,b] -- CMD...",
		Short: "Run a command with secrets injected as env vars",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return kcerrors.ErrUsage
			}
			items, err := a.Resolver.List(cmd.Context())
			if err != nil {
				return err
			}
			filter := map[string]struct{}{}
			if len(only) > 0 {
				for _, part := range only {
					for _, n := range strings.Split(part, ",") {
						n = strings.TrimSpace(n)
						if n != "" {
							filter[strings.ToLower(n)] = struct{}{}
						}
					}
				}
			}
			// Never pass kc's own identity or agent identity to the child.
			// The child only needs the secret variables injected below.
			env := stripEnvKeys(os.Environ(), "KC_KEY", "KC_AGENT")
			for _, it := range items {
				if len(filter) > 0 {
					if _, ok := filter[strings.ToLower(it.Name)]; !ok {
						continue
					}
				}
				val, _, err := a.Resolver.Get(readCtx(cmd, "exec"), it.Name)
				if err != nil {
					switch kcerrors.Code(err) {
					case kcerrors.ExitNotFound:
						continue
					case kcerrors.ExitDenied:
						fmt.Fprintf(os.Stderr, "kc: 정책에 의해 '%s' 읽기가 거부되었습니다. 사용자에게 허용 목록 변경을 요청하세요.\n", it.Name)
						return err
					default:
						// Never run the child without secrets when the store is
						// locked/blocked or a backend fails.
						return err
					}
				}
				for _, key := range a.Resolver.EnvNames(it.Name) {
					env = append(env, fmt.Sprintf("%s=%s", key, string(val)))
				}
			}
			if opencode.ShouldSyncBackgroundService(args) {
				if key := opencode.EffectiveProxyAPIKey(env); key != "" {
					if err := opencode.SyncBackgroundServiceEnv(args[0], key); err != nil {
						return err
					}
				}
			}
			c := exec.Command(args[0], args[1:]...)
			c.Env = env
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				if exit, ok := err.(*exec.ExitError); ok {
					return ExitCodeError{Code: exit.ExitCode()}
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&only, "only", nil, "Only inject these secret names")
	return cmd
}

// stripEnvKeys removes exact KEY=... entries from an environment slice.
func stripEnvKeys(env []string, keys ...string) []string {
	drop := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		drop[k] = struct{}{}
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		key, _, ok := strings.Cut(e, "=")
		if ok {
			if _, skip := drop[key]; skip {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}
