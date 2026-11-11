package journal

import (
	"errors"
	"fmt"

	"github.com/camem2442/keycli/internal/authoritybinding"
)

// MigratePendingV1 records a host-verified mapping for a legacy operation.
// Verify must establish the exact original operation and canonical resource
// using trusted metadata. Migration preserves the original state and cannot
// dispatch or settle an effect.
func (s Store) MigratePendingV1(id string, verify func(Intent) (authoritybinding.Binding, error)) (Record, error) {
	if !ValidOperationID(id) || verify == nil {
		return Record{}, errors.New("invalid delivery journal migration")
	}
	dir, _, unlock, err := s.lock(false)
	if err != nil {
		return Record{}, err
	}
	defer unlock()

	current, err := readRecord(dir, id)
	if err != nil {
		return Record{}, err
	}
	if current.Version != 3 && (current.Version != 1 || !pending(current.State)) {
		return Record{}, ErrIntentMismatch
	}
	binding, err := verify(current.Intent)
	if err != nil {
		return Record{}, err
	}
	mapped := current
	mapped.Version = 3
	mapped.Binding = &binding
	mapped.ResourceDigest = binding.ResourceDigest
	if !validBinding(mapped) || (current.Version == 3 && *current.Binding != binding) {
		return Record{}, ErrIntentMismatch
	}
	backupName := "." + id + ".v1"
	backup, err := readNamedRecord(dir, id, backupName)
	switch {
	case errors.Is(err, ErrNotFound) && current.Version == 1:
		if err := writeNamedRecord(dir, backupName, current); err != nil {
			return Record{}, fmt.Errorf("preserve original delivery record: %w", err)
		}
	case err != nil:
		return Record{}, err
	case backup.Version != 1 || backup.Intent != current.Intent || !pending(backup.State) ||
		(current.Version == 1 && backup.State != current.State):
		return Record{}, ErrIntentMismatch
	}
	if current.Version == 3 {
		return unresolved(current), nil
	}
	if err := writeRecord(dir, mapped); err != nil {
		return Record{}, fmt.Errorf("persist delivery resource mapping: %w", err)
	}
	return unresolved(mapped), nil
}
