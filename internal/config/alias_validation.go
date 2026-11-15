package config

import (
	"fmt"
	"sort"
	"strings"
)

// AliasErrors rejects names that can resolve to more than one canonical owner.
// Canonical names participate too: an alias must not shadow another primary
// name. Repeated aliases belonging to the same owner remain compatible.
func (c Config) AliasErrors() []string {
	owners := make(map[string]map[string]struct{})
	var out []string
	for canonical, aliases := range c.Alias {
		for _, raw := range append([]string{canonical}, aliases...) {
			name := strings.ToLower(strings.TrimSpace(raw))
			if name == "" {
				out = append(out, fmt.Sprintf("alias.%s: empty name", canonical))
				continue
			}
			if owners[name] == nil {
				owners[name] = make(map[string]struct{})
			}
			owners[name][canonical] = struct{}{}
		}
	}
	for name, entries := range owners {
		if len(entries) < 2 {
			continue
		}
		names := make([]string, 0, len(entries))
		for canonical := range entries {
			names = append(names, canonical)
		}
		sort.Strings(names)
		out = append(out, fmt.Sprintf("alias %q has multiple canonical owners: %s", name, strings.Join(names, ", ")))
	}
	sort.Strings(out)
	return out
}

// CanonicalName returns the primary name for any alias (case-insensitive).
func (c Config) CanonicalName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return name
	}
	for canon, aliases := range c.Alias {
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
