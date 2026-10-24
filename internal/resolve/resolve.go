package resolve

import (
	"context"
	"fmt"
	"strings"

	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/env"
	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/backend/keychain"
	"github.com/camem2442/keycli/internal/backend/link"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/middleware"
)

// Resolver maps names through aliases and queries backends in order.
type Resolver struct {
	cfg      config.Config
	backends []backend.Backend
	lookup   middleware.Lookup
	policy   *middleware.Policy
}

// New builds a resolver.
func New(cfg config.Config, backends []backend.Backend, home string, warn func(string)) *Resolver {
	r := &Resolver{cfg: cfg, backends: backends}
	r.policy = middleware.NewPolicy(cfg, "")
	inner := r.lookupBackends
	mws := middleware.BuildChain(home, cfg, warn)
	if len(mws) > 0 {
		r.lookup = middleware.Chain(mws, inner)
	} else {
		r.lookup = inner
	}
	return r
}

// PolicyAllowed reports whether the current agent may read canonicalName.
func (r *Resolver) PolicyAllowed(canonicalName string) bool {
	if r.policy == nil {
		return true
	}
	return r.policy.Allowed(canonicalName)
}

// CurrentAgent returns the detected or overridden agent id.
func (r *Resolver) CurrentAgent() string {
	if r.policy == nil {
		return middleware.DetectAgent()
	}
	return r.policy.Agent()
}

// CanonicalName returns the primary name for any alias (case-insensitive).
func (r *Resolver) CanonicalName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return name
	}
	for canon, aliases := range r.cfg.Alias {
		if strings.ToLower(canon) == n {
			return canon
		}
		for _, a := range aliases {
			if strings.ToLower(a) == n {
				return canon
			}
		}
	}
	return name
}

// EnvNames returns environment variable names to set for a canonical secret name.
func (r *Resolver) EnvNames(canonical string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToUpper(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	add(canonical)
	if aliases, ok := r.cfg.Alias[canonical]; ok {
		for _, a := range aliases {
			add(a)
		}
	}
	return out
}

// Get resolves name and returns value plus backend instance name.
func (r *Resolver) Get(ctx context.Context, name string) ([]byte, string, error) {
	val, backendName, _, err := r.GetWithSource(ctx, name)
	return val, backendName, err
}

// GetWithSource also returns a stable source label (never includes secret values).
func (r *Resolver) GetWithSource(ctx context.Context, name string) ([]byte, string, string, error) {
	if r.lookup != nil {
		val, src, err := r.lookup(ctx, name)
		if err != nil {
			return nil, "", "", err
		}
		backendName := backendFromSource(src)
		return val, backendName, src, nil
	}
	return r.lookupBackendsWithMeta(ctx, name)
}

func (r *Resolver) lookupBackends(ctx context.Context, name string) ([]byte, string, error) {
	val, _, src, err := r.lookupBackendsWithMeta(ctx, name)
	return val, src, err
}

func (r *Resolver) lookupBackendsWithMeta(ctx context.Context, name string) ([]byte, string, string, error) {
	canon := r.CanonicalName(name)
	tryNames := []string{canon, name}
	if aliases, ok := r.cfg.Alias[canon]; ok {
		tryNames = append(tryNames, aliases...)
	}
	for _, b := range r.backends {
		if b.Caps().NeedsNetwork && r.networkSkip() {
			continue
		}
		ok, _, err := b.Available(ctx)
		if err != nil {
			if kcerrors.Code(err) == kcerrors.ExitBlocked {
				continue
			}
			return nil, "", "", err
		}
		if !ok {
			continue
		}
		for _, n := range tryNames {
			val, src, err := r.getFromBackend(ctx, b, n)
			if err == nil {
				if src == "" {
					src = b.Kind() + ":" + b.InstanceName()
				}
				return val, b.InstanceName(), src, nil
			}
			if !isNotFound(err) {
				return nil, "", "", err
			}
		}
	}
	return nil, "", "", fmt.Errorf("%w: %s", kcerrors.ErrNotFound, canon)
}

func backendFromSource(src string) string {
	if src == "" {
		return ""
	}
	if i := strings.Index(src, ":"); i > 0 {
		return src[:i]
	}
	return ""
}

func (r *Resolver) getFromBackend(ctx context.Context, b backend.Backend, name string) ([]byte, string, error) {
	switch x := b.(type) {
	case *env.Env:
		return x.GetWithSource(ctx, name)
	case *link.Link:
		val, err := x.Get(ctx, name)
		if err != nil {
			return nil, "", err
		}
		return val, x.SourceFor(name), nil
	case *keychain.Keychain:
		return x.GetWithSource(ctx, name)
	default:
		val, err := b.Get(ctx, name)
		return val, "", err
	}
}

func (r *Resolver) networkSkip() bool {
	return false
}

// ListItem is one entry for ls / ls --json.
type ListItem struct {
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	Backend      string   `json:"backend"`
	Source       string   `json:"source,omitempty"`
	Note         string   `json:"note"`
	Expires      *string  `json:"expires"`
	StaleExport  bool     `json:"stale_export,omitempty"`
	PolicyDenied bool     `json:"policy_denied,omitempty"`
}

// List aggregates metadata from all backends.
func (r *Resolver) List(ctx context.Context) ([]ListItem, error) {
	byName := map[string]ListItem{}
	for _, b := range r.backends {
		if b.Caps().NeedsNetwork && r.networkSkip() {
			continue
		}
		ok, _, err := b.Available(ctx)
		if err != nil || !ok {
			continue
		}
		if !b.Caps().CanList {
			continue
		}
		items, err := b.List(ctx)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			canon := r.CanonicalName(it.Name)
			li, exists := byName[strings.ToLower(canon)]
			if !exists {
				aliases := r.cfg.Alias[canon]
				if aliases == nil {
					aliases = []string{}
				}
				var exp *string
				if it.Expires != nil {
					s := it.Expires.Format("2006-01-02")
					exp = &s
				}
				src := ""
				if lk, ok := b.(*link.Link); ok {
					src = lk.SourceFor(canon)
				}
				li = ListItem{
					Name:    canon,
					Aliases: append([]string{}, aliases...),
					Backend: b.InstanceName(),
					Source:  src,
					Note:    it.Note,
					Expires: exp,
				}
				byName[strings.ToLower(canon)] = li
			}
		}
	}
	out := make([]ListItem, 0, len(byName))
	for _, li := range byName {
		if middleware.HasRules(r.cfg) && !r.PolicyAllowed(li.Name) {
			li.PolicyDenied = true
		}
		out = append(out, li)
	}
	return out, nil
}

// Set writes to the first writable available backend.
func (r *Resolver) Set(ctx context.Context, name string, value []byte, meta backend.Meta) error {
	canon := r.CanonicalName(name)
	for _, b := range r.backends {
		if !b.Caps().CanWrite {
			continue
		}
		ok, reason, err := b.Available(ctx)
		if err != nil {
			return err
		}
		if !ok {
			if reason != "" {
				continue
			}
			continue
		}
		return b.Set(ctx, canon, value, meta)
	}
	return fmt.Errorf("no writable backend available")
}

// Delete removes from writable backends that hold the name.
func (r *Resolver) Delete(ctx context.Context, name string) error {
	canon := r.CanonicalName(name)
	var last error
	found := false
	for _, b := range r.backends {
		if !b.Caps().CanWrite {
			continue
		}
		ok, _, err := b.Available(ctx)
		if err != nil || !ok {
			continue
		}
		err = b.Delete(ctx, canon)
		if err == nil {
			found = true
			continue
		}
		if isNotFound(err) {
			continue
		}
		last = err
	}
	if last != nil {
		return last
	}
	if !found {
		return fmt.Errorf("%w: %s", kcerrors.ErrNotFound, canon)
	}
	return nil
}

func isNotFound(err error) bool {
	return kcerrors.Code(err) == kcerrors.ExitNotFound
}

// FileStore returns the first file backend in the resolver chain, if any.
func (r *Resolver) FileStore() *file.Store {
	for _, b := range r.backends {
		if fs, ok := b.(*file.Store); ok {
			return fs
		}
	}
	return nil
}

// HasEnvBackend reports whether env is configured in backend order.
func (r *Resolver) HasEnvBackend() bool {
	for _, b := range r.backends {
		if b.Kind() == "env" {
			return true
		}
	}
	return false
}
