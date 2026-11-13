package target

import (
	"os"
	"strings"
)

// CompatibilityPushEnv preserves the native tool's authentication environment
// while excluding KeyCLI's local store and Gateway controls.
func CompatibilityPushEnv() []string {
	env := os.Environ()
	out := env[:0]
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "KC_KEY", "KC_AGENT", "KC_EFFECT_KEY", "KC_PROVIDER_EFFECTS":
			continue
		}
		out = append(out, entry)
	}
	return out
}
