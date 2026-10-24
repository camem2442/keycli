package keychain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// ExecRunner runs /usr/bin/security on the host.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, securityBin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w (%s)", securityBin, err, stderr.String())
	}
	return out, nil
}

func (ExecRunner) RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, securityBin, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w (%s)", securityBin, err, stderr.String())
	}
	return out, nil
}

// FakeRunner maps service names to values for Linux unit tests.
type FakeRunner struct {
	Secrets map[string]string
	Calls   []string
}

func (f *FakeRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	f.Calls = append(f.Calls, args[0])
	switch args[0] {
	case "find-generic-password":
		service := argValue(args, "-s")
		if v, ok := f.Secrets[service]; ok {
			return []byte(v), nil
		}
		return nil, fmt.Errorf("not found")
	case "delete-generic-password":
		service := argValue(args, "-s")
		if _, ok := f.Secrets[service]; !ok {
			return nil, fmt.Errorf("not found")
		}
		delete(f.Secrets, service)
		return nil, nil
	case "dump-keychain":
		var b bytes.Buffer
		for s := range f.Secrets {
			fmt.Fprintf(&b, "service: %s\n", s)
		}
		return b.Bytes(), nil
	default:
		return nil, fmt.Errorf("unexpected: %v", args)
	}
}

func (f *FakeRunner) RunWithStdin(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	f.Calls = append(f.Calls, "add-generic-password")
	if args[0] != "add-generic-password" {
		return nil, fmt.Errorf("unexpected")
	}
	service := argValue(args, "-s")
	if service == "" {
		return nil, fmt.Errorf("missing service")
	}
	if stringsHasPrefix(service, "local.study.ai.") {
		return nil, fmt.Errorf("denied")
	}
	f.Secrets[service] = string(stdin)
	return nil, nil
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func argValue(args []string, flag string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// Ensure ExecRunner uses os executable
var _ = os.Getenv
