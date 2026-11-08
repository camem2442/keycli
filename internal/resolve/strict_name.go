package resolve

import (
	"errors"
	"strings"
)

// StrictCanonicalName resolves a security-bound name only when the complete
// alias configuration is unambiguous. Canonical names and aliases share one
// case-insensitive namespace, matching CanonicalName and policy lookup.
//
// This stricter opt-in path leaves legacy resolution unchanged. Validating the
// entire alias map also excludes ambiguous fallback aliases and canonical-name
// shadowing. With the invocation's unchanged in-memory configuration, a later
// Get(canonical) and policy lookup cannot select another canonical item.
func (r *Resolver) StrictCanonicalName(name string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return "", errors.New("empty attested item name")
	}
	owners := make(map[string]string)
	bind := func(value, canonical string) error {
		// Do not accept spellings that legacy resolution and backend fallback may
		// normalize differently. No configuration or requested name is modified.
		if value == "" || strings.TrimSpace(value) != value {
			return errors.New("invalid attested alias configuration")
		}
		for _, ch := range value {
			if ch < 0x20 || ch == 0x7f {
				return errors.New("invalid attested alias configuration")
			}
		}
		key := strings.ToLower(value)
		if owner, exists := owners[key]; exists && owner != canonical {
			return errors.New("ambiguous attested alias configuration")
		}
		owners[key] = canonical
		return nil
	}
	for canonical, aliases := range r.cfg.Alias {
		if err := bind(canonical, canonical); err != nil {
			return "", err
		}
		for _, alias := range aliases {
			if err := bind(alias, canonical); err != nil {
				return "", err
			}
		}
	}
	if canonical, ok := owners[normalized]; ok {
		return canonical, nil
	}
	return name, nil
}
