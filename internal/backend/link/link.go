package link

import (
	"context"
	"fmt"
	"strings"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/keychain"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
)

func init() {
	backend.Register("link", func(ctx context.Context, instanceName string, spec any) (backend.Backend, error) {
		args, ok := spec.(backend.BackendBuildArgs)
		if !ok {
			return nil, fmt.Errorf("link: invalid build args")
		}
		runner := args.Deps.SecurityRunner
		if runner == nil {
			runner = keychain.ExecRunner{Config: args.Deps.Config}
		}
		return &Link{
			instance: instanceName,
			links:    args.Deps.Config.Link,
			runner:   runner,
			cfg:      args.Deps.Config,
		}, nil
	})
}

// Link resolves configured keychain links (read-only).
type Link struct {
	instance string
	links    map[string]config.LinkTarget
	runner   backend.SecurityRunner
	cfg      config.Config
}

func (l *Link) InstanceName() string { return l.instance }
func (l *Link) Kind() string         { return "link" }
func (l *Link) Caps() backend.Capabilities {
	return backend.Capabilities{CanList: len(l.links) > 0}
}

func (l *Link) Available(ctx context.Context) (bool, string, error) {
	if len(l.links) == 0 {
		return false, "no links configured", nil
	}
	return true, "", nil
}

func (l *Link) Get(ctx context.Context, name string) ([]byte, error) {
	canon := strings.ToLower(strings.TrimSpace(name))
	for k, tgt := range l.links {
		if strings.ToLower(k) != canon {
			continue
		}
		val, _, err := keychain.FindGenericPassword(ctx, l.runner, tgt.Service, tgt.Account)
		return val, err
	}
	return nil, fmt.Errorf("%w", kcerrors.ErrNotFound)
}

// SourceFor returns a stable source label for a linked name.
func (l *Link) SourceFor(name string) string {
	canon := strings.ToLower(strings.TrimSpace(name))
	for k, tgt := range l.links {
		if strings.ToLower(k) != canon {
			continue
		}
		return keychain.FormatSource("link:keychain", tgt.Service, tgt.Account)
	}
	return ""
}

func (l *Link) List(ctx context.Context) ([]backend.Item, error) {
	var items []backend.Item
	for name := range l.links {
		items = append(items, backend.Item{Name: name})
	}
	return items, nil
}

func (l *Link) Set(ctx context.Context, name string, value []byte, meta backend.Meta) error {
	return fmt.Errorf("link backend is read-only; use kc link")
}

func (l *Link) Delete(ctx context.Context, name string) error {
	return fmt.Errorf("link backend is read-only; use kc unlink")
}
