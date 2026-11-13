package rotation

import (
	"bytes"
	"context"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

// ActiveGenerationObservation binds a fresh host observation of the current
// credential and the consumer deployments to its immutable provider identity.
type ActiveGenerationObservation struct {
	IdentityDigest string
	ObservedAt     time.Time
}

type ObserveActiveGeneration func(context.Context, PreparationPlan) (ActiveGenerationObservation, error)

// ReplacementPlan continues a verified retirement by preparing a later
// generation from the independently observed active generation.
func ReplacementPlan(ctx context.Context, store journal.Store, planID string, next Generation,
	verify SessionVerifiers, observeActive ObserveActiveGeneration) (PreparationPlan, error) {
	if ctx == nil || ctx.Err() != nil || observeActive == nil {
		return PreparationPlan{}, ErrActionNotCurrent
	}
	snapshot, err := LoadSession(store, planID)
	if err != nil {
		return PreparationPlan{}, err
	}
	session := snapshot.Session
	if session.NewIdentityDigest == "" {
		return PreparationPlan{}, ErrActionNotCurrent
	}
	plan, err := NewPreparationPlan(session.Plan.New, next, session.Plan.Consumers)
	if err != nil {
		return PreparationPlan{}, err
	}
	state, err := session.State(ctx, store, time.Now(), verify)
	if err != nil {
		return PreparationPlan{}, err
	}
	if state != SessionRevokeVerified {
		return PreparationPlan{}, ErrActionNotCurrent
	}
	started := time.Now()
	active, err := observeActive(ctx, clonePreparationPlan(session.Plan))
	if err != nil || ctx.Err() != nil || active.IdentityDigest != session.NewIdentityDigest ||
		active.ObservedAt.Before(started) || active.ObservedAt.After(time.Now()) {
		return PreparationPlan{}, ErrActionNotCurrent
	}
	current, err := LoadSession(store, planID)
	if err != nil {
		return PreparationPlan{}, err
	}
	if !bytes.Equal(current.raw, snapshot.raw) {
		return PreparationPlan{}, journal.ErrMetadataChanged
	}
	return plan, nil
}
