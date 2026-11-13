package rotation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

func TestVerifiedRetirementStartsFreshPreparation(t *testing.T) {
	ctx := context.Background()
	store := rotationStore(t)
	plan := planFixture(t)
	grant := strings.Repeat("a", 64)
	newIdentity := strings.Repeat("b", 64)
	intent := func(id string) journal.Intent {
		return journal.Intent{OperationID: id, TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: grant}
	}
	stage, revoke := intent("first-stage"), intent("first-revoke")
	session := Session{Version: 1, Plan: plan, OldIdentityDigest: strings.Repeat("c", 64), NewIdentityDigest: newIdentity,
		Stage: stage, Revoke: &revoke, RevocationEvidence: strings.Repeat("d", 64)}
	if _, err := SaveSession(store, SessionSnapshot{}, session); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Dispatch(revoke, func() (journal.Observation, error) {
		return journal.Observation{State: journal.Acknowledged, ProofDigest: grant}, nil
	}); err != nil {
		t.Fatal(err)
	}
	next := plan.New
	next.Number++
	activeReads := 0
	active := func(_ context.Context, current PreparationPlan) (ActiveGenerationObservation, error) {
		activeReads++
		if current.ID != plan.ID {
			t.Fatalf("observed a different plan: %+v", current)
		}
		return ActiveGenerationObservation{IdentityDigest: newIdentity, ObservedAt: time.Now()}, nil
	}
	if _, err := ReplacementPlan(ctx, store, plan.ID, next, SessionVerifiers{}, active); !errors.Is(err, ErrActionNotCurrent) || activeReads != 0 {
		t.Fatalf("unverified retirement started replacement: %v, reads=%d", err, activeReads)
	}
	verify := SessionVerifiers{Revocation: func(_ context.Context, _ Session, record journal.Record) error {
		if record.Intent != revoke {
			return errors.New("wrong retirement record")
		}
		return nil
	}}
	if _, err := ReplacementPlan(ctx, store, plan.ID, plan.Old, verify, active); err == nil {
		t.Fatal("retired generation accepted as replacement")
	}
	if _, err := ReplacementPlan(ctx, store, plan.ID, next, verify,
		func(context.Context, PreparationPlan) (ActiveGenerationObservation, error) {
			return ActiveGenerationObservation{IdentityDigest: session.OldIdentityDigest, ObservedAt: time.Now()}, nil
		}); !errors.Is(err, ErrActionNotCurrent) {
		t.Fatalf("different active identity accepted: %v", err)
	}
	replacement, err := ReplacementPlan(ctx, store, plan.ID, next, verify, active)
	if err != nil || replacement.Old != plan.New || replacement.New != next || replacement.ID == plan.ID {
		t.Fatalf("replacement plan: %+v, %v", replacement, err)
	}
	stageCalls := 0
	record, err := Prepare(ctx, store, "replacement-stage", replacement,
		func(context.Context, string, PreparationPlan) (PreparationGrant, error) {
			return PreparationGrant{PlanDigest: replacement.ID, GrantDigest: grant, ExpiresAt: time.Now().Add(time.Minute)}, nil
		}, func(_ context.Context, p PreparationPlan) (journal.Observation, error) {
			stageCalls++
			if p.Old != plan.New || p.New != next {
				t.Fatal("staging reused the retired generation")
			}
			return journal.Observation{State: journal.Acknowledged, ProofDigest: grant}, nil
		})
	if err != nil || record.State != journal.Acknowledged || stageCalls != 1 || activeReads != 1 {
		t.Fatalf("stage record=%+v calls=%d reads=%d error=%v", record, stageCalls, activeReads, err)
	}
}
