//go:build !darwin && !linux

package hostbridge

import (
	"context"
	"os"
	"time"
)

func (a *GuardedExecution) exchange(context.Context, AuthorityBinding, time.Time, func() time.Time, *os.File) error {
	return ErrLocalAuthorityUnavailable
}
