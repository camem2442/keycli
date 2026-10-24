package gh

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/dotenv"
	"github.com/camem2442/keycli/internal/target"
	"github.com/camem2442/keycli/internal/testhooks"
)

// Options for gh secret set.
type Options struct {
	User    bool
	Repos   []string
	App     string // codespaces | actions | agents
	DryRun  bool
	GhBin   string
	ArgvLog string // test hook: log argv without secrets
}

// Push sets user/org secrets via `gh secret set -f -` (value on stdin only).
func Push(ctx context.Context, opts Options, cfg config.Config, secrets []target.Secret) error {
	ghBin := opts.GhBin
	if ghBin == "" {
		ghBin = testhooks.Env(cfg, "KC_GH_BIN")
	}
	if ghBin == "" {
		ghBin = "gh"
	}
	app := opts.App
	if app == "" {
		app = "codespaces"
	}
	for _, sec := range secrets {
		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "kc: [dry-run] gh secret set %s (stdin)\n", sec.Name)
			continue
		}
		if err := setOne(ctx, ghBin, opts, app, sec); err != nil {
			return err
		}
	}
	return nil
}

func setOne(ctx context.Context, ghBin string, opts Options, app string, sec target.Secret) error {
	args := []string{"secret", "set", sec.Name, "-f", "-"}
	if opts.User {
		args = append(args, "--user")
	}
	for _, r := range opts.Repos {
		args = append(args, "--repos", r)
	}
	if app != "" {
		args = append(args, "--app", app)
	}
	if opts.ArgvLog != "" {
		_ = os.WriteFile(opts.ArgvLog, []byte(strings.Join(args, " ")+"\n"), 0o644)
	}
	cmd := exec.CommandContext(ctx, ghBin, args...)
	cmd.Stdin = strings.NewReader(dotenv.Line(sec.Name, sec.Value))
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh: %v: %s", err, strings.TrimSpace(errBuf.String()))
	}
	return nil
}
