package dotenv

import (
	"fmt"
	"sort"
	"strings"
)

// Line formats one KEY=value line for dotenv paste (no trailing newline on single line).
func Line(name string, value []byte) string {
	return fmt.Sprintf("%s=%s", envKey(name), quoteValue(string(value)))
}

// Format joins multiple secrets as dotenv text.
func Format(secrets map[string][]byte) string {
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	var lines []string
	for _, name := range names {
		lines = append(lines, Line(name, secrets[name]))
	}
	return strings.Join(lines, "\n") + "\n"
}

func envKey(name string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(name), "-", "_"))
}

func quoteValue(s string) string {
	if needsQuotes(s) {
		return fmt.Sprintf("%q", escapeQuotes(s))
	}
	return s
}

func needsQuotes(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '#', '"', '\'', '=', '$', '`':
			return true
		}
	}
	return strings.ContainsAny(s, " \t#\"'=")
}

func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, "\"", "\\\"")
}
