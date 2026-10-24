package external

import (
	"context"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/config"
)

func init() {
	backend.Register("external", func(ctx context.Context, instanceName string, spec any) (backend.Backend, error) {
		args, ok := spec.(backend.BackendBuildArgs)
		if !ok {
			return nil, fmt.Errorf("external: invalid build args")
		}
		return New(ctx, instanceName, args)
	})
}

// Backend talks to a kc-backend-* executable.
type Backend struct {
	instance       string
	bin            string
	runner         Runner
	timeout        time.Duration
	caps           backend.Capabilities
	handshook      bool
	networkBlocked func() bool
}

// New builds an external backend from config.
func New(ctx context.Context, instanceName string, args backend.BackendBuildArgs) (*Backend, error) {
	spec := args.Spec
	plugin := spec.Plugin
	if plugin == "" && spec.Command != "" {
		plugin = PluginNameFromBinary(spec.Command)
	}
	bin, err := ResolveCommand(args.Deps.Home, plugin, spec.Command)
	if err != nil {
		return nil, err
	}
	timeout := 30 * time.Second
	if spec.Timeout != "" {
		d, err := time.ParseDuration(spec.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout for %q: %w", instanceName, err)
		}
		timeout = d
	}
	var runner Runner
	if args.Deps.PluginRunner != nil {
		if r, ok := args.Deps.PluginRunner.(Runner); ok {
			runner = r
		}
	}
	if runner == nil {
		runner = ExecRunner{Timeout: timeout}
	}
	b := &Backend{
		instance:       instanceName,
		bin:            bin,
		runner:         runner,
		timeout:        timeout,
		networkBlocked: args.Deps.NetworkBlocked,
	}
	if err := b.refreshCaps(ctx); err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Backend) InstanceName() string { return b.instance }
func (b *Backend) Kind() string         { return "external" }

func (b *Backend) Caps() backend.Capabilities { return b.caps }

func (b *Backend) Available(ctx context.Context) (bool, string, error) {
	if b.caps.NeedsNetwork && b.networkBlocked != nil && b.networkBlocked() {
		return false, "network blocked", nil
	}
	if !b.handshook {
		if err := b.refreshCaps(ctx); err != nil {
			return false, "", err
		}
	}
	return true, "", nil
}

func (b *Backend) refreshCaps(ctx context.Context) error {
	resp, err := b.runner.Run(ctx, b.bin, Request{
		V:       ProtocolVersion,
		Op:      "handshake",
		Backend: b.instance,
	})
	if err != nil {
		return err
	}
	if err := CheckResponse(resp); err != nil {
		return err
	}
	b.caps = backend.Capabilities{
		CanWrite:     resp.Capabilities.CanWrite,
		CanList:      resp.Capabilities.CanList,
		Interactive:  resp.Capabilities.Interactive,
		NeedsNetwork: resp.Capabilities.NeedsNetwork,
	}
	b.handshook = true
	return nil
}

func (b *Backend) Get(ctx context.Context, name string) ([]byte, error) {
	resp, err := b.runner.Run(ctx, b.bin, Request{V: ProtocolVersion, Op: "get", Name: name, Backend: b.instance})
	if err != nil {
		return nil, err
	}
	return DecodeValue(resp)
}

func (b *Backend) List(ctx context.Context) ([]backend.Item, error) {
	resp, err := b.runner.Run(ctx, b.bin, Request{V: ProtocolVersion, Op: "list", Backend: b.instance})
	if err != nil {
		return nil, err
	}
	if err := CheckResponse(resp); err != nil {
		return nil, err
	}
	var items []backend.Item
	for _, it := range resp.Items {
		var exp *time.Time
		if it.Expires != nil && *it.Expires != "" {
			t, err := time.Parse("2006-01-02", *it.Expires)
			if err == nil {
				exp = &t
			}
		}
		items = append(items, backend.Item{Name: it.Name, Note: it.Note, Expires: exp})
	}
	return items, nil
}

func (b *Backend) Set(ctx context.Context, name string, value []byte, meta backend.Meta) error {
	if !b.caps.CanWrite {
		return fmt.Errorf("external backend %q is read-only", b.instance)
	}
	var exp *string
	if meta.Expires != nil {
		s := meta.Expires.Format("2006-01-02")
		exp = &s
	}
	resp, err := b.runner.Run(ctx, b.bin, Request{
		V:           ProtocolVersion,
		Op:          "set",
		Backend:     b.instance,
		Name:        name,
		ValueBase64: base64.StdEncoding.EncodeToString(value),
		Note:        meta.Note,
		Expires:     exp,
	})
	if err != nil {
		return err
	}
	return CheckResponse(resp)
}

func (b *Backend) Delete(ctx context.Context, name string) error {
	if !b.caps.CanWrite {
		return fmt.Errorf("external backend %q is read-only", b.instance)
	}
	resp, err := b.runner.Run(ctx, b.bin, Request{V: ProtocolVersion, Op: "delete", Name: name, Backend: b.instance})
	if err != nil {
		return err
	}
	return CheckResponse(resp)
}

// CommandPath exposes the resolved binary for plugin list.
func (b *Backend) CommandPath() string { return b.bin }

// Handshake probes capabilities without constructing a full backend.
func Handshake(ctx context.Context, runner Runner, bin string, instance string) (Capabilities, int, error) {
	resp, err := runner.Run(ctx, bin, Request{
		V:       ProtocolVersion,
		Op:      "handshake",
		Backend: instance,
	})
	if err != nil {
		return Capabilities{}, 0, err
	}
	if err := CheckResponse(resp); err != nil {
		return Capabilities{}, 0, err
	}
	pv := resp.Protocol
	if pv == 0 {
		pv = ProtocolVersion
	}
	return resp.Capabilities, pv, nil
}

// EnabledBackends maps plugin short name → config instance names.
func EnabledBackends(cfg config.Config) map[string][]string {
	out := map[string][]string{}
	for inst, spec := range cfg.Backend {
		if spec.Type != "external" {
			continue
		}
		if spec.Enabled != nil && !*spec.Enabled {
			continue
		}
		name := spec.Plugin
		if name == "" {
			name = PluginNameFromBinary(spec.Command)
		}
		if name == "" {
			continue
		}
		out[name] = append(out[name], inst)
	}
	return out
}

// IsPluginEnabled reports whether a discovered plugin name is referenced in config.
func IsPluginEnabled(cfg config.Config, pluginName string) bool {
	enabled := EnabledBackends(cfg)
	_, ok := enabled[pluginName]
	return ok
}
