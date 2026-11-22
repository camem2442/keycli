// Package browserintent defines a narrow, secret-free request envelope for a
// future Mac-owned browser/passkey worker. This package does NOT grant access.
package browserintent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

const maxRequestBytes = 4096

var ErrRejected = errors.New("browserintent: request rejected")

// Intent never includes a password, passkey, cookie, CDP port or browser profile.
// The original independent workspace-infra authority must verify these claims.
type Intent struct {
	Version     int    `json:"v"`
	Goal        string `json:"goal"`
	Claim       string `json:"claim"`
	RequestID   string `json:"request_id"`
	Site        string `json:"site"`
	Action      string `json:"action"`
	Path        string `json:"path"`
	ExpiresUnix int64  `json:"expires_unix"`
}

var allowed = map[string]bool{
	"v": true, "goal": true, "claim": true, "request_id": true,
	"site": true, "action": true, "path": true, "expires_unix": true,
}

// Decode rejects extra/duplicate JSON keys and requests larger than 4 KiB.
// Reject reasons deliberately do not echo input, which may be hostile text.
func Decode(r io.Reader, now time.Time) (Intent, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxRequestBytes+1))
	if err != nil || len(b) > maxRequestBytes {
		return Intent{}, ErrRejected
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	t, err := dec.Token()
	if err != nil || t != json.Delim('{') {
		return Intent{}, ErrRejected
	}
	raw := map[string]json.RawMessage{}
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return Intent{}, ErrRejected
		}
		k, ok := token.(string)
		if !ok || !allowed[k] {
			return Intent{}, ErrRejected
		}
		if _, already := raw[k]; already {
			return Intent{}, ErrRejected
		}
		var val json.RawMessage
		if dec.Decode(&val) != nil {
			return Intent{}, ErrRejected
		}
		raw[k] = val
	}
	t, err = dec.Token()
	if err != nil || t != json.Delim('}') {
		return Intent{}, ErrRejected
	}
	if _, err = dec.Token(); err != io.EOF {
		return Intent{}, ErrRejected
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return Intent{}, ErrRejected
	}
	var intent Intent
	if json.Unmarshal(encoded, &intent) != nil || intent.Validate(now) != nil {
		return Intent{}, ErrRejected
	}
	return intent, nil
}

// Validate is a syntactic precondition only; it is not authority verification.
func (i Intent) Validate(now time.Time) error {
	if i.Version != 1 || !validID(i.Goal) || !validID(i.Claim) || !validID(i.RequestID) || !validSite(i.Site) {
		return ErrRejected
	}
	if i.Action != "session_status" && i.Action != "read_page" && i.Action != "reauthenticate" {
		return ErrRejected
	}
	// No arbitrary JavaScript, network scheme, open redirects, or query fields.
	if len(i.Path) == 0 || len(i.Path) > 512 || !strings.HasPrefix(i.Path, "/") || strings.HasPrefix(i.Path, "//") || strings.ContainsAny(i.Path, "\\%?#@") || strings.Contains(i.Path, "..") {
		return ErrRejected
	}
	for _, c := range i.Path {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || strings.ContainsRune("/-_.", c)) {
			return ErrRejected
		}
	}
	expiry := time.Unix(i.ExpiresUnix, 0)
	if !expiry.After(now) || expiry.After(now.Add(5*time.Minute)) {
		return ErrRejected
	}
	return nil
}

func validID(s string) bool {
	if len(s) < 8 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validSite(site string) bool {
	if len(site) < 3 || len(site) > 253 || site[0] == '.' || site[len(site)-1] == '.' || !strings.Contains(site, ".") {
		return false
	}
	for _, s := range strings.Split(site, ".") {
		if len(s) == 0 || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
			return false
		}
		for _, c := range s {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

// Authority is implemented only by the independently admitted owner host.
// VerifyAndReserve MUST validate the original Goal/Grant/Claim and atomically
// reserve request_id, not trust any user-supplied "verified" boolean.
type Authority interface {
	VerifyAndReserve(context.Context, Intent) error
}

// Browser is the native isolated owner-host worker. It never returns raw
// browser state, cookie values or page data to this interface.
type Browser interface {
	Perform(context.Context, Intent) (Outcome, error)
}

// Outcome is deliberately constrained to fixed status values.
type Outcome string

const (
	// A browser action acknowledgement is never proof of an authenticated
	// session. These outcomes describe independently observed session state.
	SessionPresent Outcome = "session_present"
	NoSession      Outcome = "no_session"
	NeedsHuman     Outcome = "human_auth_required"
	// Historical generic ACK, retained for callers but never accepted by Gate.
	Performed Outcome = "performed"
)

// OutcomeFromOwnerStatus translates only the fixed statuses emitted by the
// owner Chrome adapter. The bridge never returns page HTML, cookies or OTP.
func OutcomeFromOwnerStatus(action, status string) (Outcome, error) {
	switch {
	case status == "SESSION_PRESENT" &&
		(action == "session_status" || action == "reauthenticate"):
		return SessionPresent, nil
	case status == "NO_SESSION" && action == "session_status":
		return NoSession, nil
	case status == "HUMAN_ACTION_REQUIRED" && action == "reauthenticate":
		return NeedsHuman, nil
	default:
		return "", ErrRejected
	}
}

// Gate requires both an independent authority and browser executor. There is
// no offline bypass; nil components fail closed.
func Gate(ctx context.Context, now time.Time, in Intent, authority Authority, browser Browser) (Outcome, error) {
	if ctx.Err() != nil || in.Validate(now) != nil || authority == nil || browser == nil ||
		in.Action == "read_page" { // no scoped/redacted output worker exists
		return "", ErrRejected
	}
	if authority.VerifyAndReserve(ctx, in) != nil || ctx.Err() != nil {
		return "", ErrRejected
	}
	result, err := browser.Perform(ctx, in)
	if err != nil {
		return "", ErrRejected
	}
	// The browser must report a current observed session, not merely
	// that a command was accepted. A NO_SESSION read is not a 2FA request.
	switch in.Action {
	case "session_status":
		if result == SessionPresent || result == NoSession {
			return result, nil
		}
	case "reauthenticate":
		if result == SessionPresent || result == NeedsHuman {
			return result, nil
		}
	}
	return "", ErrRejected
}
