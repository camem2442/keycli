package rotation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

// Session stores host metadata and references to independent evidence.
// Journal records remain authoritative for every attempted effect.
type Session struct {
	Version            int                  `json:"v"`
	Plan               PreparationPlan      `json:"plan"`
	OldIdentityDigest  string               `json:"old_identity_digest"`
	NewIdentityDigest  string               `json:"new_identity_digest,omitempty"`
	Stage              journal.Intent       `json:"stage"`
	Activate           *journal.Intent      `json:"activate,omitempty"`
	Revoke             *journal.Intent      `json:"revoke,omitempty"`
	Rollback           *journal.Intent      `json:"rollback,omitempty"`
	Restoration        *RestorationEvidence `json:"restoration,omitempty"`
	StageEvidence      string               `json:"stage_evidence,omitempty"`
	Activation         *ActivationEvidence  `json:"activation,omitempty"`
	Consumers          []ConsumerEvidence   `json:"consumers,omitempty"`
	RevocationEvidence string               `json:"revocation_evidence,omitempty"`
}

type SessionSnapshot struct {
	Session Session
	raw     []byte
}

// LoadSession reloads the metadata without replaying any journal operation.
func LoadSession(store journal.Store, planID string) (SessionSnapshot, error) {
	raw, err := store.ReadRotationMetadata(planID)
	if err != nil {
		return SessionSnapshot{}, err
	}
	var session Session
	if err := json.Unmarshal(raw, &session); err != nil || session.Plan.ID != planID {
		return SessionSnapshot{}, errors.New("invalid rotation session")
	}
	canonical, err := encodeSession(session)
	if err != nil || !bytes.Equal(raw, canonical) {
		return SessionSnapshot{}, errors.New("invalid or noncanonical rotation session")
	}
	return SessionSnapshot{Session: session, raw: raw}, nil
}

// SaveSession writes a validated snapshot. Updates use the snapshot returned
// by LoadSession so concurrent changes cannot silently discard evidence.
func SaveSession(store journal.Store, previous SessionSnapshot, session Session) (SessionSnapshot, error) {
	raw, err := encodeSession(session)
	if err != nil {
		return SessionSnapshot{}, err
	}
	if len(previous.raw) != 0 {
		var original Session
		if json.Unmarshal(previous.raw, &original) != nil || original.Plan.ID != session.Plan.ID ||
			original.OldIdentityDigest != session.OldIdentityDigest || original.Stage != session.Stage ||
			(original.NewIdentityDigest != "" && original.NewIdentityDigest != session.NewIdentityDigest) ||
			!preservesOperation(original.Activate, session.Activate) || !preservesOperation(original.Revoke, session.Revoke) ||
			!preservesOperation(original.Rollback, session.Rollback) {
			return SessionSnapshot{}, errors.New("rotation session identity changed")
		}
	}
	if err := store.ReplaceRotationMetadata(session.Plan.ID, previous.raw, raw); err != nil {
		return SessionSnapshot{}, err
	}
	var snapshot Session
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return SessionSnapshot{}, err
	}
	return SessionSnapshot{Session: snapshot, raw: raw}, nil
}

func preservesOperation(prior, next *journal.Intent) bool {
	return prior == nil || next != nil && *prior == *next
}

func encodeSession(s Session) ([]byte, error) {
	invalid := errors.New("invalid rotation session metadata")
	plan, err := NewPreparationPlan(s.Plan.Old, s.Plan.New, s.Plan.Consumers)
	if err != nil || s.Version != 1 || plan.Version != s.Plan.Version || plan.ID != s.Plan.ID ||
		!slices.Equal(plan.Consumers, s.Plan.Consumers) || !journal.ValidDigest(s.OldIdentityDigest) ||
		(s.NewIdentityDigest != "" && (!journal.ValidDigest(s.NewIdentityDigest) || s.NewIdentityDigest == s.OldIdentityDigest)) {
		return nil, invalid
	}
	seen := make(map[string]bool)
	for _, op := range []*journal.Intent{&s.Stage, s.Activate, s.Revoke, s.Rollback} {
		if op == nil {
			continue
		}
		if !journal.ValidOperationID(op.OperationID) || !journal.ValidDigest(op.TargetDigest) ||
			!journal.ValidDigest(op.GrantDigest) || op.PlanDigest != plan.ID || seen[op.OperationID] {
			return nil, invalid
		}
		seen[op.OperationID] = true
	}
	if s.Restoration != nil && (s.Rollback == nil || !validRestoration(s.Plan, *s.Restoration)) {
		return nil, invalid
	}
	if s.StageEvidence != "" && (!journal.ValidDigest(s.StageEvidence) || s.NewIdentityDigest == "") ||
		s.RevocationEvidence != "" && (!journal.ValidDigest(s.RevocationEvidence) || s.Revoke == nil) {
		return nil, invalid
	}
	if s.Activation != nil {
		a := s.Activation
		if s.Activate == nil || !journal.ValidDigest(s.StageEvidence) || a.StageReceiptDigest != s.StageEvidence ||
			!journal.ValidDigest(a.PromotionReceiptDigest) || a.ActivatedAt.IsZero() || !a.GraceUntil.After(a.ActivatedAt) {
			return nil, invalid
		}
	}
	if len(s.Consumers) > len(plan.Consumers) || (len(s.Consumers) > 0 && s.Activation == nil) {
		return nil, invalid
	}
	seen = make(map[string]bool)
	for _, e := range s.Consumers {
		if seen[e.ConsumerID] || !slices.ContainsFunc(plan.Consumers, func(c Consumer) bool { return c.ID == e.ConsumerID }) ||
			e.Generation != plan.New.Number || e.ObservedAt.Before(s.Activation.ActivatedAt) ||
			!journal.ValidDigest(e.EvidenceDigest) {
			return nil, invalid
		}
		seen[e.ConsumerID] = true
	}
	return json.Marshal(s)
}

type SessionState string

const (
	SessionPlanned            SessionState = "PLANNED"
	SessionPreparedUnverified SessionState = "PREPARED_UNVERIFIED"
	SessionStageVerified      SessionState = "STAGE_VERIFIED"
	SessionActiveVerified     SessionState = "ACTIVE_VERIFIED"
	SessionRetirementReady    SessionState = "RETIREMENT_READY"
	SessionRevokeAcknowledged SessionState = "REVOKE_ACKNOWLEDGED"
	SessionRevokeVerified     SessionState = "REVOKE_VERIFIED"
	SessionUnknownEffect      SessionState = "UNKNOWN_EFFECT"
)

// SessionVerifiers must independently corroborate the stored metadata against
// trusted host or provider records. A digest or journal acknowledgement alone
// never verifies a stage, activation, consumer, or revocation.
type SessionVerifiers struct {
	Stage       func(context.Context, Session, journal.Record) error
	Activation  VerifyActivation
	Consumer    VerifyConsumer
	Revocation  func(context.Context, Session, journal.Record) error
	Restoration VerifyRestoration
}

// State derives a read-only view. It neither dispatches nor authorizes work.
func (s Session) State(ctx context.Context, store journal.Store, now time.Time, verify SessionVerifiers) (SessionState, error) {
	if ctx == nil || ctx.Err() != nil || now.IsZero() {
		return "", errors.New("rotation status context unavailable")
	}
	if _, err := encodeSession(s); err != nil {
		return "", err
	}
	var stage, activate, revoke, rollback journal.Record
	refs := []struct {
		intent *journal.Intent
		record *journal.Record
	}{{&s.Stage, &stage}, {s.Activate, &activate}, {s.Revoke, &revoke}, {s.Rollback, &rollback}}
	for _, ref := range refs {
		if ref.intent == nil {
			continue
		}
		record, err := store.Status(ref.intent.OperationID)
		if errors.Is(err, journal.ErrNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}
		if record.Intent != *ref.intent {
			return "", journal.ErrIntentMismatch
		}
		switch record.State {
		case journal.IntentWritten, journal.Dispatched, journal.UnknownEffect:
			return SessionUnknownEffect, nil
		}
		*ref.record = record
	}
	if effectObserved(revoke) {
		if s.RevocationEvidence != "" && verify.Revocation != nil && verify.Revocation(ctx, s, revoke) == nil {
			return SessionRevokeVerified, nil
		}
		return SessionRevokeAcknowledged, nil
	}
	if effectObserved(rollback) {
		if s.Restoration != nil && verify.Restoration != nil &&
			freshRestoration(*s.Restoration, now) && verify.Restoration(ctx, clonePreparationPlan(s.Plan), rollback, cloneRestoration(*s.Restoration)) == nil {
			return SessionActiveVerified, nil
		}
		return SessionPreparedUnverified, nil
	}
	if !effectObserved(stage) {
		return SessionPlanned, nil
	}
	if s.StageEvidence == "" || verify.Stage == nil || verify.Stage(ctx, s, stage) != nil {
		return SessionPreparedUnverified, nil
	}
	if !effectObserved(activate) || s.Activation == nil || verify.Activation == nil ||
		verify.Activation(ctx, clonePreparationPlan(s.Plan), *s.Activation) != nil {
		return SessionStageVerified, nil
	}
	if verify.Consumer != nil {
		_, decision, err := Attest(ctx, s.Plan, *s.Activation, s.Consumers, now,
			func(context.Context, PreparationPlan, ActivationEvidence) error { return nil }, verify.Consumer)
		if err == nil && decision.Status == RetirementCandidate {
			return SessionRetirementReady, nil
		}
	}
	return SessionActiveVerified, nil
}

func effectObserved(record journal.Record) bool {
	return record.State == journal.Acknowledged || record.State == journal.ObservedApplied
}
