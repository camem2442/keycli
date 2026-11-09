package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/ciguard"
	"github.com/spf13/cobra"
)

// newCIGuardCmd runs only the SHA256-pinned, read-only Workspace Infrastructure
// guard. It never calls workflow_dispatch, changes repository state, or promotes
// Ready/merge. A protected github_agent secret is used only for live GETs.
func newCIGuardCmd(a *app.App) *cobra.Command {
	var script, sha, request, snapshot, python string
	cmd := &cobra.Command{
		Use:   "ci-guard",
		Short: "Read-only pre-dispatch duplicate check (not dispatch authority)",
		RunE: func(cmd *cobra.Command, args []string) error {
			pinned, err := ciguard.VerifyScript(script, sha)
			if err != nil {
				return err
			}
			if request == "" {
				return errors.New("--request is required")
			}
			if python == "" {
				return errors.New("--python must name a trusted interpreter")
			}
			// Execute a private copy of verified bytes, not the mutable source path.
			dir, err := os.MkdirTemp("", "kc-ci-guard-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			staged := filepath.Join(dir, "ci_dispatch_guard.py")
			if err := os.WriteFile(staged, pinned, 0400); err != nil {
				return err
			}
			argv := []string{"-I", "-B", staged, "--request", request}
			env := stripEnvKeys(os.Environ(), "KC_KEY", "KC_AGENT", "GH_TOKEN", "GITHUB_TOKEN", "GH_AGENT_TOKEN")
			if snapshot == "" {
				// Only the dedicated low-privilege token is admitted here.
				// Never fall back to ambient gh credentials or broader tokens.
				secret, _, err := a.Resolver.Get(readCtx(cmd, "ci-guard"), "github_agent")
				if err != nil {
					return err
				}
				if len(secret) == 0 {
					return errors.New("github_agent credential unavailable")
				}
				env = append(env, "GH_TOKEN="+string(secret))
				argv = append(argv, "--github")
			} else {
				argv = append(argv, "--snapshot", snapshot)
			}
			child := exec.CommandContext(cmd.Context(), python, argv...)
			child.Env = env
			child.Stdin = nil
			child.Stdout = cmd.OutOrStdout()
			child.Stderr = cmd.ErrOrStderr()
			if err := child.Run(); err != nil {
				var status *exec.ExitError
				if errors.As(err, &status) {
					return ExitCodeError{Code: status.ExitCode()}
				}
				return fmt.Errorf("read-only guard unavailable: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&script, "script", "", "Absolute path to trusted workspace-infra guard script")
	cmd.Flags().StringVar(&sha, "script-sha256", "", "SHA256 pin of the reviewed guard script")
	cmd.Flags().StringVar(&request, "request", "", "Bounded JSON identity request (PR/HEAD/base/workflow/scope/env)")
	cmd.Flags().StringVar(&snapshot, "snapshot", "", "Offline fixture; cannot authorize deduplication")
	cmd.Flags().StringVar(&python, "python", "python3", "Trusted Python executable")
	return cmd
}
