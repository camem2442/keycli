package config

import (
	"sort"
	"strings"
)

// NormalizeGitHost lowercases a credential host and drops any scheme, path or
// userinfo that may have been pasted into config.
func NormalizeGitHost(host string) string {
	h := strings.TrimSpace(strings.ToLower(host))
	if i := strings.Index(h, "://"); i >= 0 {
		h = h[i+3:]
	}
	if i := strings.Index(h, "/"); i >= 0 {
		h = h[:i]
	}
	if i := strings.Index(h, "@"); i >= 0 {
		h = h[i+1:]
	}
	return h
}

// GitHost returns the git credential spec for host (case-insensitive).
//
// Explicit [git."HOST"] entries win. When no explicit github.com mapping
// exists, the built-in default is returned with Implicit set so callers can
// distinguish "configured" from "works when the secret happens to exist".
func (c Config) GitHost(host string) (GitHostSpec, bool) {
	want := NormalizeGitHost(host)
	if want == "" {
		return GitHostSpec{}, false
	}
	if spec, ok := c.explicitGitHost(want); ok {
		return spec, true
	}
	if want == defaultGitHost {
		return defaultGitSpec(), true
	}
	return GitHostSpec{}, false
}

// explicitGitHost looks up only mappings written in config.
func (c Config) explicitGitHost(host string) (GitHostSpec, bool) {
	if spec, ok := c.Git[host]; ok {
		return spec, true
	}
	for name, spec := range c.Git {
		if NormalizeGitHost(name) == host {
			return spec, true
		}
	}
	return GitHostSpec{}, false
}

// EnsureGitHost records an explicit mapping for host when none exists and
// reports whether config changed. setup-git uses this so the host it
// configures is enforced (missing secret is an error) instead of relying on
// the implicit, silent default.
func (c *Config) EnsureGitHost(host string) (GitHostSpec, bool) {
	host = NormalizeGitHost(host)
	if host == "" {
		return GitHostSpec{}, false
	}
	if spec, ok := c.explicitGitHost(host); ok {
		return spec, false
	}
	secret := host
	if host == defaultGitHost {
		secret = "github"
	}
	spec := GitHostSpec{Secret: secret, Username: "git"}
	if c.Git == nil {
		c.Git = map[string]GitHostSpec{}
	}
	c.Git[host] = spec
	return spec, true
}

// GitHosts returns configured hosts, normalized, deduplicated and sorted.
func (c Config) GitHosts() []string {
	out := make([]string, 0, len(c.Git))
	seen := map[string]struct{}{}
	for name := range c.Git {
		h := NormalizeGitHost(name)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}
