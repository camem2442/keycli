package keychain

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"github.com/camem2442/keycli/internal/config"
)

// ExecRunner runs security(1) on the host.
type ExecRunner struct {
	Config config.Config
}

func (e ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	return e.runSecurity(ctx, nil, args...)
}

func (e ExecRunner) RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	if len(args) > 0 && args[0] == "add-generic-password" {
		return e.runAddGenericPassword(ctx, stdin, args...)
	}
	return e.runSecurity(ctx, stdin, args...)
}

func (e ExecRunner) runAddGenericPassword(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	service := flagValue(args, "-s")
	account := flagValue(args, "-a")
	if service == "" || account == "" {
		return e.runSecurity(ctx, stdin, args...)
	}
	update := false
	for _, a := range args {
		if a == "-U" {
			update = true
		}
	}
	password := stdin
	if len(password) == 0 {
		return nil, fmt.Errorf("missing password")
	}
	var line strings.Builder
	line.WriteString("add-generic-password -s ")
	line.WriteString(service)
	line.WriteString(" -a ")
	line.WriteString(account)
	if update {
		line.WriteString(" -U")
	}
	line.WriteString(" -X ")
	line.WriteString(hex.EncodeToString(password))
	line.WriteByte('\n')
	return e.runSecurity(ctx, []byte(line.String()), "-i")
}

func (e ExecRunner) runSecurity(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	bin := SecurityCommand(e.Config)
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

func flagValue(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}
