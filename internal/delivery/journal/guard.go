package journal

import (
	"errors"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

// DispatchGuard borrows the writer lock during a bound dispatch callback.
type DispatchGuard struct {
	state *dispatchGuardState
}

type dispatchGuardState struct {
	mu   sync.Mutex
	lock *os.File
}

func runDispatch(lock *os.File, effect func(DispatchGuard) (Observation, error)) (Observation, error) {
	state := &dispatchGuardState{lock: lock}
	defer func() {
		state.mu.Lock()
		defer state.mu.Unlock()
		state.lock = nil
	}()
	return effect(DispatchGuard{state: state})
}

// WithLockFile lends a duplicate of the actual held lock to a synchronous,
// trusted verifier. Any receiver must promptly close its copy before reporting
// a verdict and must never unlock the shared open file description.
func (g DispatchGuard) WithLockFile(verify func(*os.File) error) error {
	if g.state == nil || verify == nil {
		return errors.New("delivery journal dispatch guard unavailable")
	}
	g.state.mu.Lock()
	defer g.state.mu.Unlock()
	if g.state.lock == nil {
		return errors.New("delivery journal dispatch guard expired")
	}
	raw, err := g.state.lock.SyscallConn()
	if err != nil {
		return err
	}
	var fd int
	var duplicateErr error
	if err := raw.Control(func(lockFD uintptr) {
		fd, duplicateErr = unix.FcntlInt(lockFD, unix.F_DUPFD_CLOEXEC, 0)
	}); err != nil {
		return err
	}
	if duplicateErr != nil {
		return duplicateErr
	}
	file := os.NewFile(uintptr(fd), "delivery-journal-lock")
	defer file.Close()
	return verify(file)
}
