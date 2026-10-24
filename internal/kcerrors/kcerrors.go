package kcerrors

import "errors"

// Exit code contract (docs/contracts/cli.md).
const (
	ExitOK       = 0
	ExitError    = 1
	ExitNotFound = 2
	ExitBlocked  = 3
	ExitLocked   = 4
	ExitDenied   = 5
	ExitUsage    = 64
)

var (
	ErrNotFound = errors.New("not found")
	ErrBlocked  = errors.New("blocked")
	ErrLocked   = errors.New("locked")
	ErrDenied   = errors.New("denied")
	ErrUsage    = errors.New("usage")
)

// Code maps known errors to exit codes; unknown errors return ExitError.
func Code(err error) int {
	if err == nil {
		return ExitOK
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return ExitNotFound
	case errors.Is(err, ErrBlocked):
		return ExitBlocked
	case errors.Is(err, ErrLocked):
		return ExitLocked
	case errors.Is(err, ErrDenied):
		return ExitDenied
	case errors.Is(err, ErrUsage):
		return ExitUsage
	default:
		return ExitError
	}
}
