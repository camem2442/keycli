package cloudflare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/target"
	"github.com/camem2442/keycli/internal/testhooks"
)

// Options for wrangler secret put.
type Options struct {
	Worker      string // Worker script name (--name)
	Env         string // wrangler environment (--env)
	Config      string // wrangler config path (--config)
	DryRun      bool
	WranglerBin string
	ArgvLog     string // test hook: log argv without secrets
}

// Push sets Worker secrets via `wrangler secret put NAME` (value on stdin only).
func Push(ctx context.Context, opts Options, cfg config.Config, secrets []target.Secret) error {
	if opts.Worker == "" && opts.Config == "" {
		return errors.New("cloudflare: --worker 또는 --config 가 필요합니다")
	}
	bin := opts.WranglerBin
	if bin == "" {
		bin = testhooks.Env(cfg, "KC_WRANGLER_BIN")
	}
	if bin == "" {
		bin = "wrangler"
	}
	for _, sec := range secrets {
		if opts.DryRun {
			fmt.Fprintf(os.Stderr, "kc: [dry-run] wrangler secret put %s%s (stdin)\n", sec.Name, scopeSuffix(opts))
			continue
		}
		if err := putOne(ctx, bin, opts, sec); err != nil {
			return err
		}
	}
	return nil
}

func scopeSuffix(opts Options) string {
	var parts []string
	if opts.Worker != "" {
		parts = append(parts, "--name "+opts.Worker)
	}
	if opts.Env != "" {
		parts = append(parts, "--env "+opts.Env)
	}
	if opts.Config != "" {
		parts = append(parts, "--config "+opts.Config)
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, " ")
}

func putOne(ctx context.Context, bin string, opts Options, sec target.Secret) error {
	args := []string{"secret", "put", sec.Name}
	if opts.Worker != "" {
		args = append(args, "--name", opts.Worker)
	}
	if opts.Env != "" {
		args = append(args, "--env", opts.Env)
	}
	if opts.Config != "" {
		args = append(args, "--config", opts.Config)
	}
	if opts.ArgvLog != "" {
		_ = os.WriteFile(opts.ArgvLog, []byte(strings.Join(args, " ")+"\n"), 0o644)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = bytes.NewReader(sec.Value)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(scrub(errBuf.String(), sec.Value))
		return fmt.Errorf("wrangler: %v: %s", err, msg)
	}
	return nil
}

// scrub removes the secret value from tool output before it is surfaced.
func scrub(s string, val []byte) string {
	if len(val) == 0 {
		return s
	}
	s = strings.ReplaceAll(s, string(val), "***")
	if trimmed := strings.TrimSpace(string(val)); trimmed != "" && trimmed != string(val) {
		s = strings.ReplaceAll(s, trimmed, "***")
	}
	return s
}
