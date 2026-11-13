// Package journal records attempts at externally authorized delivery effects.
// The caller owns admission and provider evidence. This package neither grants
// execution authority nor proves that a provider stored the intended value.
package journal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/camem2442/keycli/internal/authoritybinding"
)

type State string

const (
	IntentWritten    State = "INTENT_WRITTEN"
	Dispatched       State = "DISPATCHED"
	UnknownEffect    State = "UNKNOWN_EFFECT"
	Acknowledged     State = "ACKNOWLEDGED"
	ObservedApplied  State = "OBSERVED_APPLIED"
	ObservedNoEffect State = "OBSERVED_NO_EFFECT"
)

var (
	ErrNotFound       = errors.New("delivery operation not found")
	ErrDuplicate      = errors.New("delivery operation already exists")
	ErrIntentMismatch = errors.New("delivery operation identity mismatch")
	ErrTargetPending  = errors.New("delivery target has an unresolved operation")
	ErrBusy           = errors.New("delivery journal is locked")
	ErrUnknownEffect  = errors.New("delivery effect is unknown")
)

var operationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ValidOperationID reports whether id matches the journal operation contract.
// Exported so callers (the effect gateway) reuse this rule instead of copying it.
func ValidOperationID(id string) bool { return operationID.MatchString(id) }

// ValidDigest reports whether value matches the journal digest contract.
func ValidDigest(value string) bool { return digest.MatchString(value) }

// Intent contains identifiers only. TargetDigest must hash the canonical
// resource identity and be stable across grants, plans, and operation IDs.
// Proof and identity digests must never be derived from credential values.
type Intent struct {
	OperationID  string `json:"operation_id"`
	TargetDigest string `json:"target_digest"`
	PlanDigest   string `json:"plan_digest"`
	GrantDigest  string `json:"grant_digest"`
}

type Record struct {
	Version int `json:"v"`
	Intent
	ResourceDigest string                    `json:"resource_digest,omitempty"`
	Binding        *authoritybinding.Binding `json:"authority_binding,omitempty"`
	State          State                     `json:"state"`
	ProofDigest    string                    `json:"proof_digest,omitempty"`
}

// Observation is supplied only by a trusted host callback. Its digest
// references metadata evidence for this exact operation and target.
type Observation struct {
	State       State
	ProofDigest string
}

// Store owns a dedicated private directory under an existing safe parent.
// All cooperating writers of a target must use the same root and target digest.
type Store struct {
	Root string
}

// Dispatch admits only a new operation. It synchronizes the intent and
// dispatch marker before invoking effect exactly once, while holding the
// writer lock. A provider acknowledgement is not a verification receipt.
func (s Store) Dispatch(intent Intent, effect func() (Observation, error)) (Record, error) {
	if effect == nil {
		return Record{}, errors.New("invalid delivery journal dispatch")
	}
	return s.dispatch(Record{Version: 1, Intent: intent, State: IntentWritten}, func(DispatchGuard) (Observation, error) {
		return effect()
	})
}

// DispatchBound persists the protected registration projection alongside the
// intent in both durable markers and lends the held writer lock to effect.
// It grants no execution authority.
func (s Store) DispatchBound(intent Intent, binding authoritybinding.Binding, effect func(DispatchGuard) (Observation, error)) (Record, error) {
	return s.dispatch(Record{Version: 2, Intent: intent, Binding: &binding, State: IntentWritten}, effect)
}

func (s Store) dispatch(record Record, effect func(DispatchGuard) (Observation, error)) (Record, error) {
	if !validIntent(record.Intent) || !validBinding(record) || effect == nil {
		return Record{}, errors.New("invalid delivery journal dispatch")
	}
	dir, lock, unlock, err := s.lock(true)
	if err != nil {
		return Record{}, err
	}
	defer unlock()

	if err := admit(dir, record); err != nil {
		return Record{}, err
	}
	if err := writeRecord(dir, record); err != nil {
		return Record{}, fmt.Errorf("persist delivery intent: %w", err)
	}
	record.State = Dispatched
	if err := writeRecord(dir, record); err != nil {
		return unresolved(record), errors.Join(ErrUnknownEffect, fmt.Errorf("persist delivery dispatch marker: %w", err))
	}
	observation, err := runDispatch(lock, effect)
	if err != nil || !digest.MatchString(observation.ProofDigest) ||
		(observation.State != Acknowledged && observation.State != ObservedNoEffect) {
		record.State = UnknownEffect
		return record, errors.Join(ErrUnknownEffect, writeRecord(dir, record))
	}
	return settle(dir, record, observation)
}

// Status reads a single atomic snapshot. An in-flight or abandoned dispatch
// marker is evidence of a possible effect, so both are projected as unknown.
func (s Store) Status(id string) (Record, error) {
	if !operationID.MatchString(id) {
		return Record{}, errors.New("invalid delivery operation ID")
	}
	dir, err := s.open(false)
	if err != nil {
		return Record{}, err
	}
	defer dir.Close()
	record, err := readRecord(dir, id)
	return unresolved(record), err
}

// Reconcile invokes a trusted read-only observer under the writer lock.
// Only operation-bound proof closes the latch. The callback cannot request
// redispatch, and the journal has no transition back to an unattempted state.
func (s Store) Reconcile(intent Intent, observe func(Record) (Observation, error)) (Record, error) {
	if !validIntent(intent) || observe == nil {
		return Record{}, errors.New("invalid delivery journal reconciliation")
	}
	return s.reconcile(intent.OperationID, func(record Record) bool {
		return record.Intent == intent && record.Binding == nil
	}, observe)
}

// ReconcileBound resolves an operation by its persisted Authority binding.
// The host observer must authorize the read and corroborate the outcome.
func (s Store) ReconcileBound(operationID string, observe func(Record) (Observation, error)) (Record, error) {
	if !ValidOperationID(operationID) || observe == nil {
		return Record{}, errors.New("invalid bound delivery journal reconciliation")
	}
	return s.reconcile(operationID, func(record Record) bool { return record.Binding != nil }, observe)
}

func (s Store) reconcile(operationID string, matches func(Record) bool, observe func(Record) (Observation, error)) (Record, error) {
	dir, _, unlock, err := s.lock(false)
	if err != nil {
		return Record{}, err
	}
	defer unlock()
	record, err := readRecord(dir, operationID)
	if err != nil {
		return Record{}, err
	}
	if !matches(record) {
		return Record{}, ErrIntentMismatch
	}
	if !pending(record.State) {
		return record, nil
	}
	view := unresolved(record)
	if record.Binding != nil {
		binding := *record.Binding
		view.Binding = &binding
	}
	observation, err := observe(view)
	if err != nil || !digest.MatchString(observation.ProofDigest) ||
		(observation.State != ObservedNoEffect &&
			(observation.State != ObservedApplied || record.State == IntentWritten)) {
		return unresolved(record), ErrUnknownEffect
	}
	return settle(dir, record, observation)
}

func validIntent(intent Intent) bool {
	return operationID.MatchString(intent.OperationID) &&
		digest.MatchString(intent.TargetDigest) && digest.MatchString(intent.PlanDigest) &&
		digest.MatchString(intent.GrantDigest)
}

func validBinding(record Record) bool {
	b := record.Binding
	switch record.Version {
	case 1:
		return b == nil && record.ResourceDigest == ""
	case 2, 3:
		return b != nil && b.Valid() &&
			((record.Version == 2 && record.ResourceDigest == "") ||
				(record.Version == 3 && record.ResourceDigest == b.ResourceDigest)) &&
			b.MutationID == record.OperationID && b.TargetDigest == record.TargetDigest &&
			b.PlanDigest == record.PlanDigest && b.GrantDigest == record.GrantDigest
	default:
		return false
	}
}

func sameIntent(a, b Record) bool {
	if a.Intent != b.Intent {
		return false
	}
	if a.Binding == nil || b.Binding == nil {
		return a.Binding == nil && b.Binding == nil || a.Version == 3 && b.Version == 1
	}
	return *a.Binding == *b.Binding
}

func pending(state State) bool {
	return state == IntentWritten || state == Dispatched || state == UnknownEffect
}

func unresolved(record Record) Record {
	if record.State == Dispatched {
		record.State = UnknownEffect
	}
	return record
}

func settle(dir *os.File, record Record, observation Observation) (Record, error) {
	previous := record
	record.State = observation.State
	record.ProofDigest = observation.ProofDigest
	if err := writeRecord(dir, record); err != nil {
		return unresolved(previous), errors.Join(ErrUnknownEffect, fmt.Errorf("persist delivery observation: %w", err))
	}
	return record, nil
}

func admit(dir *os.File, candidate Record) error {
	record, err := readRecord(dir, candidate.OperationID)
	if err == nil {
		if !sameIntent(record, candidate) {
			return ErrIntentMismatch
		}
		return ErrDuplicate
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	for {
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			id, ok := strings.CutSuffix(entry.Name(), ".json")
			if !ok {
				continue
			}
			record, err := readRecord(dir, id)
			if err != nil {
				return fmt.Errorf("read delivery target latch: %w", err)
			}
			if pending(record.State) && conflictsWithPending(record, candidate) {
				return ErrTargetPending
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func conflictsWithPending(record, candidate Record) bool {
	if record.TargetDigest == candidate.TargetDigest {
		return true
	}
	if record.Binding == nil || candidate.Binding == nil {
		// A legacy record has no resource mapping. Preserve legacy target
		// matching between legacy callers, and fence mixed-version dispatch.
		return record.Binding != nil || candidate.Binding != nil
	}
	return record.Binding.ResourceDigest == candidate.Binding.ResourceDigest
}
