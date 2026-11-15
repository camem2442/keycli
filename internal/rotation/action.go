package rotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

// ErrActionNotCurrent indicates that local action validation prevented the
// provider mutation from being attempted.
var ErrActionNotCurrent = errors.New("credential rotation action no longer admitted")

func actionCurrent(ctx context.Context, expiresAt time.Time) bool {
	return ctx != nil && ctx.Err() == nil && time.Now().Before(expiresAt)
}

func verifyActionBoundary(ctx context.Context, expiresAt time.Time, beforeEffect func(context.Context) error) error {
	if !actionCurrent(ctx, expiresAt) {
		return ErrActionNotCurrent
	}
	if beforeEffect != nil {
		if err := beforeEffect(ctx); err != nil {
			return err
		}
		if !actionCurrent(ctx, expiresAt) {
			return ErrActionNotCurrent
		}
	}
	return nil
}

func acknowledged(observation journal.Observation, err error) (journal.Observation, error) {
	if err != nil || observation.State != journal.Acknowledged || !digestPattern.MatchString(observation.ProofDigest) {
		return journal.Observation{}, journal.ErrUnknownEffect
	}
	return observation, nil
}

func dispatchAction(store journal.Store, intent journal.Intent, execute func() (journal.Observation, error)) (journal.Record, error) {
	var actionErr error
	record, err := store.Dispatch(intent, func() (journal.Observation, error) {
		observation, err := execute()
		if !errors.Is(err, ErrActionNotCurrent) {
			return observation, err
		}
		actionErr = err
		proof := sha256.Sum256([]byte("rotation|no-effect|" + intent.OperationID))
		return journal.Observation{State: journal.ObservedNoEffect, ProofDigest: hex.EncodeToString(proof[:])}, nil
	})
	if err != nil {
		return record, err
	}
	return record, actionErr
}
