package middleware

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
)

// Policy enforces per-agent allowlists (guardrail, not a security boundary).
type Policy struct {
	cfg   config.Config
	agent string
}

// NewPolicy builds policy middleware state. agent empty uses DetectAgent().
func NewPolicy(cfg config.Config, agent string) *Policy {
	if agent == "" {
		agent = DetectAgent()
	}
	return &Policy{cfg: cfg, agent: agent}
}

// HasRules reports whether any agent policy is configured.
func HasRules(cfg config.Config) bool {
	return len(cfg.Policy.Agent) > 0
}

// PolicyEnabled reports whether policy rules are actually enforced, i.e. the
// "policy" middleware is listed in config. This is the single criterion used
// by both `ls` display and read enforcement.
func PolicyEnabled(cfg config.Config) bool {
	if !HasRules(cfg) {
		return false
	}
	for _, name := range cfg.Middleware {
		if strings.EqualFold(strings.TrimSpace(name), "policy") {
			return true
		}
	}
	return false
}

// Middleware returns a deny-by-default wrapper for agents with a configured allowlist.
func (p *Policy) Middleware() Middleware {
	if !HasRules(p.cfg) {
		return func(next Lookup) Lookup { return next }
	}
	return func(next Lookup) Lookup {
		return func(ctx context.Context, name string) ([]byte, string, error) {
			canon := canonicalForPolicy(p.cfg, name)
			if !p.Allowed(canon) {
				return nil, "", fmt.Errorf("%w: %s", kcerrors.ErrDenied, canon)
			}
			return next(ctx, name)
		}
	}
}

// Allowed reports whether name may be read under the current agent policy.
// Deny wins over Allow, so a broad allow can carry narrow exceptions.
func (p *Policy) Allowed(canonicalName string) bool {
	if !HasRules(p.cfg) {
		return true
	}
	spec, applies := p.rulesForAgent()
	if !applies {
		return true
	}
	for _, pattern := range spec.Deny {
		// A malformed deny pattern cannot be evaluated, so deny rather than
		// let the broad allow beneath it apply (fail closed).
		matched, err := matchNameErr(pattern, canonicalName)
		if err != nil || matched {
			return false
		}
	}
	for _, pattern := range spec.Allow {
		matched, err := matchNameErr(pattern, canonicalName)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// Agent returns the resolved agent id.
func (p *Policy) Agent() string {
	return p.agent
}

func (p *Policy) rulesForAgent() (config.PolicyAgentSettings, bool) {
	agents := p.cfg.Policy.Agent
	if spec, ok := agents[p.agent]; ok {
		return spec, true
	}
	// Any unregistered agent name (including explicit KC_AGENT labels) must
	// inherit the configured default guardrail, not silently bypass it.
	if spec, ok := agents["default"]; ok {
		return spec, true
	}
	return config.PolicyAgentSettings{}, false
}

func canonicalForPolicy(cfg config.Config, name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return name
	}
	for canon, aliases := range cfg.Alias {
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

// matchNameErr reports whether pattern matches name, and whether the pattern is
// a usable glob. Callers must treat a non-nil error as fail-closed: a malformed
// pattern cannot be evaluated, so it must not fall through to the allow list.
func matchNameErr(pattern, name string) (bool, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false, nil
	}
	if pattern == name {
		return true, nil
	}
	if strings.ContainsAny(pattern, "*?[") {
		return path.Match(strings.ToLower(pattern), strings.ToLower(name))
	}
	return strings.EqualFold(pattern, name), nil
}

// denyAll denies every read. Used when the policy config cannot be evaluated.
func denyAll() Middleware {
	return func(next Lookup) Lookup {
		return func(ctx context.Context, name string) ([]byte, string, error) {
			return nil, "", fmt.Errorf("%w: %s", kcerrors.ErrDenied, name)
		}
	}
}
