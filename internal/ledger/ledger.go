package ledger

import "time"

// Entry records a push export without storing plaintext (M4+).
type Entry struct {
	Name       string    `json:"name"`
	Target     string    `json:"target"`
	HMAC       string    `json:"hmac"`
	ExportedAt time.Time `json:"exported_at"`
}

// Path returns the default ledger file path (~/.kc/ledger.json).
func Path(home string) string {
	if home == "" {
		home = "."
	}
	return home + "/.kc/ledger.json"
}
