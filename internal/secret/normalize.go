package secret

import "strings"

// NormalizeSecret trims surrounding whitespace and removes every CR/LF rune
// from the secret body (PATs and API keys are single-line).
func NormalizeSecret(b []byte) []byte {
	s := strings.TrimSpace(string(b))
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	return []byte(s)
}
