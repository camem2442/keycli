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
func (p *Policy) Allowed(canonicalName string) bool {
	if !HasRules(p.cfg) {
		return true
	}
	spec, applies := p.rulesForAgent()
	if !applies {
		return true
	}
	for _, pattern := range spec.Allow {
		if matchName(pattern, canonicalName) {
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
	if p.agent == "human" {
		if spec, ok := agents["human"]; ok {
			return spec, true
		}
	}
	if p.agent == "unknown" || p.agent == "human" || p.agent == "ci" {
		if spec, ok := agents["default"]; ok {
			return spec, true
		}
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

func matchName(pattern, name string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if pattern == name {
		return true
	}
	if strings.ContainsAny(pattern, "*?[") {
		ok, err := path.Match(strings.ToLower(pattern), strings.ToLower(name))
		return err == nil && ok
	}
	return strings.EqualFold(pattern, name)
}
