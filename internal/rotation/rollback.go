package rotation

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

type RollbackGrant struct {
	PlanDigest          string
	GrantDigest         string
	OldCredentialHandle string
	ExpiresAt           time.Time
}

type AuthorizeRollback func(context.Context, string, PreparationPlan) (RollbackGrant, error)

// VerifyRestorableCredential independently verifies that the old provider
// identity is still usable for every required consumer.
type VerifyRestorableCredential func(context.Context, PreparationPlan, string) (VerifiedOldCredential, error)

type RestoreConsumers func(context.Context, PreparationPlan, string) (journal.Observation, error)

type RestorationEvidence struct {
	ActivatedAt   time.Time
	ReceiptDigest string
	Consumers     []ConsumerEvidence
}

type CollectRestoration func(context.Context, PreparationPlan, journal.Record) (RestorationEvidence, error)
type VerifyRestoration func(context.Context, PreparationPlan, journal.Record, RestorationEvidence) error

// Rollback restores the old generation under one journal dispatch. The host
// must have already enrolled the rollback operation in the session.
func Rollback(ctx context.Context, store journal.Store, planID string, authorize AuthorizeRollback,
	verifyOld VerifyRestorableCredential, restore RestoreConsumers) (journal.Record, error) {
	if ctx == nil || ctx.Err() != nil || authorize == nil || verifyOld == nil || restore == nil {
		return journal.Record{}, ErrActionNotCurrent
	}
	snapshot, err := LoadSession(store, planID)
	if err != nil {
		return journal.Record{}, err
	}
	s := snapshot.Session
	if s.Rollback == nil || s.Rollback.TargetDigest != targetDigest(s.Plan) || s.Activate == nil || s.Activation == nil {
		return journal.Record{}, ErrActionNotCurrent
	}
	grant, err := authorize(ctx, s.Rollback.OperationID, clonePreparationPlan(s.Plan))
	if err != nil || grant.PlanDigest != planID || grant.GrantDigest != s.Rollback.GrantDigest ||
		!namePattern.MatchString(grant.OldCredentialHandle) || !actionCurrent(ctx, grant.ExpiresAt) {
		return journal.Record{}, ErrActionNotCurrent
	}
	ctx, cancel := context.WithDeadline(ctx, grant.ExpiresAt)
	defer cancel()
	return dispatchAction(store, *s.Rollback, func() (journal.Observation, error) {
		current, err := LoadSession(store, planID)
		if err != nil || !bytes.Equal(current.raw, snapshot.raw) || !actionCurrent(ctx, grant.ExpiresAt) {
			return journal.Observation{}, ErrActionNotCurrent
		}
		activation, err := store.Status(s.Activate.OperationID)
		if err != nil || activation.Intent != *s.Activate || !effectObserved(activation) || !noEffectOrAbsent(store, s.Revoke) {
			return journal.Observation{}, ErrActionNotCurrent
		}
		started := time.Now()
		identity, err := verifyOld(ctx, clonePreparationPlan(s.Plan), grant.OldCredentialHandle)
		now := time.Now()
		if err != nil || identity.ObservedAt.Before(started) || identity.ObservedAt.After(now) ||
			validOldCredentialIdentity(s.Plan, grant.OldCredentialHandle, identity, now) != nil ||
			RevocationIdentityDigest(identity) != s.OldIdentityDigest || !actionCurrent(ctx, grant.ExpiresAt) {
			return journal.Observation{}, ErrActionNotCurrent
		}
		return acknowledged(restore(ctx, clonePreparationPlan(s.Plan), grant.OldCredentialHandle))
	})
}

// ConfirmRollback can retry evidence collection after process restart. It
// only reads provider and consumer metadata and never restores them again.
func ConfirmRollback(ctx context.Context, store journal.Store, planID string,
	collect CollectRestoration, verify VerifyRestoration) (SessionSnapshot, error) {
	if ctx == nil || ctx.Err() != nil || collect == nil || verify == nil {
		return SessionSnapshot{}, ErrActionNotCurrent
	}
	snapshot, err := LoadSession(store, planID)
	if err != nil {
		return SessionSnapshot{}, err
	}
	s := snapshot.Session
	if s.Rollback == nil {
		return SessionSnapshot{}, ErrActionNotCurrent
	}
	record, err := store.Status(s.Rollback.OperationID)
	if err != nil || record.Intent != *s.Rollback || !effectObserved(record) {
		return SessionSnapshot{}, ErrActionNotCurrent
	}
	started := time.Now()
	evidence, err := collect(ctx, clonePreparationPlan(s.Plan), record)
	if err != nil || !validRestoration(s.Plan, evidence) || !freshRestoration(evidence, time.Now()) {
		return SessionSnapshot{}, ErrActionNotCurrent
	}
	for _, consumer := range evidence.Consumers {
		if consumer.ObservedAt.Before(started) {
			return SessionSnapshot{}, ErrActionNotCurrent
		}
	}
	if verify(ctx, clonePreparationPlan(s.Plan), record, cloneRestoration(evidence)) != nil || ctx.Err() != nil {
		return SessionSnapshot{}, ErrActionNotCurrent
	}
	s.Restoration = &evidence
	return SaveSession(store, snapshot, s)
}

// CheckRollbackFence is evaluated while the caller owns the journal dispatch
// lock. An unresolved or applied restoration invalidates the revocation path.
func CheckRollbackFence(store journal.Store, planID string) error {
	snapshot, err := LoadSession(store, planID)
	if errors.Is(err, journal.ErrNotFound) {
		return nil
	}
	if err != nil || !noEffectOrAbsent(store, snapshot.Session.Rollback) {
		return ErrActionNotCurrent
	}
	return nil
}

func noEffectOrAbsent(store journal.Store, intent *journal.Intent) bool {
	if intent == nil {
		return true
	}
	record, err := store.Status(intent.OperationID)
	return errors.Is(err, journal.ErrNotFound) ||
		(err == nil && record.Intent == *intent && record.State == journal.ObservedNoEffect)
}

func validRestoration(plan PreparationPlan, e RestorationEvidence) bool {
	if e.ActivatedAt.IsZero() || !journal.ValidDigest(e.ReceiptDigest) || len(e.Consumers) != len(plan.Consumers) {
		return false
	}
	seen := make(map[string]bool, len(e.Consumers))
	for _, c := range e.Consumers {
		if seen[c.ConsumerID] || c.Generation != plan.Old.Number || c.ObservedAt.Before(e.ActivatedAt) ||
			!journal.ValidDigest(c.EvidenceDigest) || !slices.ContainsFunc(plan.Consumers, func(required Consumer) bool {
			return required.Required && required.ID == c.ConsumerID
		}) {
			return false
		}
		seen[c.ConsumerID] = true
	}
	return true
}

func freshRestoration(e RestorationEvidence, now time.Time) bool {
	if e.ActivatedAt.After(now) {
		return false
	}
	for _, c := range e.Consumers {
		if c.ObservedAt.After(now) || now.Sub(c.ObservedAt) > 15*time.Minute {
			return false
		}
	}
	return true
}

func cloneRestoration(e RestorationEvidence) RestorationEvidence {
	e.Consumers = slices.Clone(e.Consumers)
	return e
}
