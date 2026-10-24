package keychain

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/kcerrors"
)

const securityBin = "/usr/bin/security"

func init() {
	backend.Register("keychain", func(ctx context.Context, instanceName string, spec any) (backend.Backend, error) {
		args, ok := spec.(backend.BackendBuildArgs)
		if !ok {
			return nil, fmt.Errorf("keychain: invalid build args")
		}
		runner := args.Deps.SecurityRunner
		if runner == nil {
			runner = ExecRunner{}
		}
		prefix := args.Spec.ServicePrefix
		if prefix == "" {
			prefix = "local.study.ai"
		}
		return &Keychain{
			instance:         instanceName,
			servicePrefix:    prefix,
			runner:           runner,
			allowOnNonDarwin: args.Deps.AllowKeychainOnNonDarwin || os.Getenv("KC_TEST_KEYCHAIN") == "1",
		}, nil
	})
}

// Keychain uses macOS /usr/bin/security (injectable runner for tests).
type Keychain struct {
	instance         string
	servicePrefix    string
	runner           backend.SecurityRunner
	allowOnNonDarwin bool
}

func (k *Keychain) InstanceName() string { return k.instance }
func (k *Keychain) Kind() string         { return "keychain" }
func (k *Keychain) Caps() backend.Capabilities {
	return backend.Capabilities{
		CanWrite:    true,
		CanList:     true,
		Interactive: true,
	}
}

func (k *Keychain) Available(ctx context.Context) (bool, string, error) {
	if runtime.GOOS != "darwin" && !k.allowOnNonDarwin {
		return false, "이 환경엔 키체인이 없음", nil
	}
	if k.allowOnNonDarwin {
		return true, "", nil
	}
	if _, err := os.Stat(SecurityCommand()); err != nil {
		return false, "이 환경엔 키체인이 없음", nil
	}
	return true, "", nil
}

func (k *Keychain) writeService(name string) string {
	name = strings.TrimSpace(name)
	if strings.Contains(name, ".") {
		return name
	}
	return "kc.secret." + name
}

func (k *Keychain) isReadOnlyService(service string) bool {
	return strings.HasPrefix(service, k.servicePrefix+".")
}

func (k *Keychain) isAICoreProtected(name string) bool {
	if ac, ok := ParseAICoreName(name); ok {
		_ = ac
		return true
	}
	return false
}

func (k *Keychain) Get(ctx context.Context, name string) ([]byte, error) {
	ok, msg, err := k.Available(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", kcerrors.ErrBlocked, msg)
	}
	if ac, ok := ParseAICoreName(name); ok {
		return AICoreLookup(ctx, k.runner, ac)
	}
	service := k.writeService(name)
	out, err := k.runner.Run(ctx, "find-generic-password", "-s", service, "-a", "kc", "-w")
	if err == nil {
		return []byte(strings.TrimSpace(string(out))), nil
	}
	if mapped := MapSecurityExit(err); errorsIsNotFound(mapped) {
		return nil, mapped
	}
	return nil, MapSecurityExit(err)
}

func (k *Keychain) List(ctx context.Context) ([]backend.Item, error) {
	ok, msg, err := k.Available(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", kcerrors.ErrBlocked, msg)
	}
	out, err := k.runner.Run(ctx, "dump-keychain")
	if err != nil {
		return nil, MapSecurityExit(err)
	}
	dump := string(out)
	var items []backend.Item
	for _, p := range ListAICoreItems(dump, k.servicePrefix) {
		items = append(items, backend.Item{Name: p})
	}
	for _, line := range strings.Split(dump, "\n") {
		if !strings.Contains(line, "service:") {
			continue
		}
		svc := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "service:"))
		if !strings.HasPrefix(svc, "kc.secret.") {
			continue
		}
		short := strings.TrimPrefix(svc, "kc.secret.")
		items = append(items, backend.Item{Name: short})
	}
	return items, nil
}

func (k *Keychain) Set(ctx context.Context, name string, value []byte, meta backend.Meta) error {
	if k.isAICoreProtected(name) {
		return fmt.Errorf("%w: ai-core keychain items are read-only", kcerrors.ErrDenied)
	}
	ok, msg, err := k.Available(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", kcerrors.ErrBlocked, msg)
	}
	service := k.writeService(name)
	if k.isReadOnlyService(service) {
		return fmt.Errorf("%w: ai-core keychain items are read-only", kcerrors.ErrDenied)
	}
	_, err = k.runner.RunWithStdin(ctx, value, "add-generic-password", "-s", service, "-a", "kc", "-U")
	return MapSecurityExit(err)
}

func (k *Keychain) Delete(ctx context.Context, name string) error {
	if k.isAICoreProtected(name) {
		return fmt.Errorf("%w: ai-core keychain items are read-only", kcerrors.ErrDenied)
	}
	ok, msg, err := k.Available(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: %s", kcerrors.ErrBlocked, msg)
	}
	service := k.writeService(name)
	_, err = k.runner.Run(ctx, "delete-generic-password", "-s", service, "-a", "kc")
	if err != nil {
		return MapSecurityExit(err)
	}
	return nil
}
