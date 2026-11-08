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
			if d := p.Evaluate(canon); !d.Allowed {
				return nil, "", &DeniedError{Name: canon, Reason: d.Reason, Rule: d.Rule}
			}
			return next(ctx, name)
		}
	}
}

// Decision explains why a name was allowed or denied.
type Decision struct {
	Allowed bool
	Reason  string // "", ReasonDenyHit, ReasonNotAllowed, ReasonInvalidPattern
	Rule    string // the pattern that produced the decision
}

// Denial reasons reported in ls --json, policy check and the audit log.
const (
	ReasonDenyHit        = "deny_hit"
	ReasonNotAllowed     = "not_allowed"
	ReasonInvalidPattern = "invalid_pattern"
)

// DeniedError carries the policy decision so callers can report the rule
// instead of a bare "denied".
type DeniedError struct {
	Name   string
	Reason string
	Rule   string
}

func (e *DeniedError) Error() string {
	if e.Rule == "" {
		return fmt.Sprintf("%s: %s", kcerrors.ErrDenied, e.Name)
	}
	return fmt.Sprintf("%s: %s (%s %q)", kcerrors.ErrDenied, e.Name, e.Reason, e.Rule)
}

// Unwrap keeps errors.Is(err, kcerrors.ErrDenied) working.
func (e *DeniedError) Unwrap() error { return kcerrors.ErrDenied }

// Allowed reports whether name may be read under the current agent policy.
// Deny wins over Allow, so a broad allow can carry narrow exceptions.
func (p *Policy) Allowed(canonicalName string) bool {
	return p.Evaluate(canonicalName).Allowed
}

// Evaluate returns the decision and the rule behind it.
func (p *Policy) Evaluate(canonicalName string) Decision {
	if !HasRules(p.cfg) {
		return Decision{Allowed: true}
	}
	spec, applies := p.rulesForAgent()
	if !applies {
		return Decision{Allowed: true}
	}
	for _, pattern := range spec.Deny {
		// A malformed deny pattern cannot be evaluated, so deny rather than
		// let the broad allow beneath it apply (fail closed).
		matched, err := matchNameErr(pattern, canonicalName)
		if err != nil {
			return Decision{Reason: ReasonInvalidPattern, Rule: pattern}
		}
		if matched {
			return Decision{Reason: ReasonDenyHit, Rule: pattern}
		}
	}
	for _, pattern := range spec.Allow {
		matched, err := matchNameErr(pattern, canonicalName)
		if err == nil && matched {
			return Decision{Allowed: true}
		}
	}
	return Decision{Reason: ReasonNotAllowed}
}

// Agent returns the resolved agent id.
func (p *Policy) Agent() string {
	return p.agent
}

func (p *Policy) rulesForAgent() (config.PolicyAgentSettings, bool) {
	// An unregistered agent name (including explicit KC_AGENT labels) inherits
	// the configured default guardrail rather than silently bypassing it.
	spec, source := EffectiveRules(p.cfg, p.agent)
	return spec, source != "none"
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
			return nil, "", &DeniedError{Name: name, Reason: ReasonInvalidPattern}
		}
	}
}

// EffectiveRules returns the allow/deny rules that apply to agent, and where
// they came from: "explicit" (named entry), "default" (fallback), or "none".
func EffectiveRules(cfg config.Config, agent string) (config.PolicyAgentSettings, string) {
	if agent == "" {
		agent = DetectAgent()
	}
	if spec, ok := cfg.Policy.Agent[agent]; ok {
		return spec, "explicit"
	}
	if spec, ok := cfg.Policy.Agent["default"]; ok {
		return spec, "default"
	}
	return config.PolicyAgentSettings{}, "none"
}
