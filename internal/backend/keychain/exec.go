package keychain

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// ExecRunner runs security(1) on the host (path from KC_SECURITY).
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return runSecurity(ctx, nil, args...)
}

func (ExecRunner) RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	return runSecurity(ctx, stdin, args...)
}

func runSecurity(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	bin := SecurityCommand()
	cmd := exec.CommandContext(ctx, bin, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		exit := 1
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		}
		op := ""
		if len(args) > 0 {
			op = args[0]
		}
		return nil, &SecurityError{ExitCode: exit, Stderr: strings.TrimSpace(stderr.String()), Op: op}
	}
	return out, nil
}
