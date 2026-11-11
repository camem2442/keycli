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
	ErrNotFound      = errors.New("delivery operation not found")
	ErrDuplicate     = errors.New("delivery operation already exists")
	ErrTargetPending = errors.New("delivery target has an unresolved operation")
	ErrBusy          = errors.New("delivery journal is locked")
	ErrUnknownEffect = errors.New("delivery effect is unknown")
)

var operationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

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
	State       State  `json:"state"`
	ProofDigest string `json:"proof_digest,omitempty"`
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
	if !validIntent(intent) || effect == nil {
		return Record{}, errors.New("invalid delivery journal dispatch")
	}
	dir, unlock, err := s.lock(true)
	if err != nil {
		return Record{}, err
	}
	defer unlock()

	if err := admit(dir, intent); err != nil {
		return Record{}, err
	}
	record := Record{Version: 1, Intent: intent, State: IntentWritten}
	if err := writeRecord(dir, record); err != nil {
		return Record{}, fmt.Errorf("persist delivery intent: %w", err)
	}
	record.State = Dispatched
	if err := writeRecord(dir, record); err != nil {
		return unresolved(record), errors.Join(ErrUnknownEffect, fmt.Errorf("persist delivery dispatch marker: %w", err))
	}
	observation, err := effect()
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
	dir, unlock, err := s.lock(false)
	if err != nil {
		return Record{}, err
	}
	defer unlock()
	record, err := readRecord(dir, intent.OperationID)
	if err != nil {
		return Record{}, err
	}
	if record.Intent != intent {
		return Record{}, errors.New("delivery operation identity mismatch")
	}
	if !pending(record.State) {
		return record, nil
	}
	observation, err := observe(unresolved(record))
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

func admit(dir *os.File, intent Intent) error {
	_, err := readRecord(dir, intent.OperationID)
	if err == nil {
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
			if record.TargetDigest == intent.TargetDigest && pending(record.State) {
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
