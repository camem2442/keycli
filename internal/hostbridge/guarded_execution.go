package hostbridge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrGuardedExecutionUnknown = errors.New("GUARDED_EXECUTION_UNKNOWN")

// GuardedExecution pins the endpoint and kernel credentials of a trusted host
// verifier. Only trusted host composition can provision it.
type GuardedExecution struct {
	path string
	uid  uint32
	pid  int
}

func NewGuardedExecution(socketPath string, authorityUID uint32, authorityPID int) (*GuardedExecution, error) {
	if !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath ||
		socketPath == "/" || len(socketPath) > 103 || strings.ContainsRune(socketPath, 0) || authorityPID <= 0 {
		return nil, ErrLocalAuthorityUnavailable
	}
	return &GuardedExecution{path: socketPath, uid: authorityUID, pid: authorityPID}, nil
}

// Verify uses the borrowed duplicate of the held journal lock. A nil error
// is an authenticated reservation claim. ErrAdmissionRejected is an
// authenticated denial, while ErrGuardedExecutionUnknown means the verifier
// may have claimed the reservation but its outcome is unavailable.
func (a *GuardedExecution) Verify(ctx context.Context, binding AuthorityBinding, expiry time.Time, now func() time.Time, lock *os.File) error {
	if a == nil || ctx == nil || ctx.Err() != nil || lock == nil {
		return ErrLocalAuthorityUnavailable
	}
	if !binding.Valid() {
		return ErrBindingMismatch
	}
	if now == nil {
		now = time.Now
	}
	if !now().Before(expiry) {
		return ErrAdmissionInvalid
	}
	return a.exchange(ctx, binding, expiry, now, lock)
}
