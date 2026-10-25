package keychain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/camem2442/keycli/internal/kcerrors"
)

// SecurityError wraps security(1) failures with its exit status.
type SecurityError struct {
	ExitCode int
	Stderr   string
	Op       string
}

func (e *SecurityError) Error() string {
	return fmt.Sprintf("security %s: exit %d: %s", e.Op, e.ExitCode, e.Stderr)
}

// MapSecurityExit maps documented security exit codes to kc exit codes.
// Sources: macOS security(1) errSecItemNotFound (-25300) → exit 44 in practice;
// user interaction / sandbox failures → non-44 errors (treated as blocked when sandbox).
func MapSecurityExit(err error) error {
	var se *SecurityError
	if !errors.As(err, &se) || se == nil {
		return err
	}
	if se.ExitCode == 44 {
		// A sandboxed process cannot reach securityd, so the keychain search
		// fails with "parameters ... not valid" and security reports the item
		// as not found. Treat that as blocked (exit 3), not as a missing name.
		if containsSandbox(se.Stderr) {
			return fmt.Errorf("%w: %s", kcerrors.ErrBlocked, se.Stderr)
		}
		return fmt.Errorf("%w", kcerrors.ErrNotFound)
	}
	if se.ExitCode == 51 || se.ExitCode == 128 || se.ExitCode == 1 {
		// 51: user canceled/denied; 128: generic failure in some sandboxes.
		if containsSandbox(se.Stderr) {
			return fmt.Errorf("%w: %s", kcerrors.ErrBlocked, se.Stderr)
		}
	}
	return err
}

func containsSandbox(stderr string) bool {
	for _, sub := range []string{
		"User interaction is not allowed",
		"interaction not allowed",
		"sandbox",
		// Observed inside sandbox-exec: the keychain search list is
		// unavailable, so SecKeychainSearchCreateFromAttributes fails.
		"One or more parameters passed to a function were not valid",
	} {
		if strings.Contains(stderr, sub) {
			return true
		}
	}
	return false
}
