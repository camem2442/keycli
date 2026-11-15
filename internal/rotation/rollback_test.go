package rotation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
)

func TestRollbackRestoresOnlyUsableOldGeneration(t *testing.T) {
	for _, scenario := range []string{
		"old generation usable", "old generation unavailable", "revocation acknowledged",
		"missing revoke enrollment", "legacy same-plan record", "bound same-plan record", "legacy no-effect record",
	} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			store := rotationStore(t)
			plan := planFixture(t)
			handle := cloudflareOldHandle()
			identity := fixtureOldCredential(plan, handle)
			digest := strings.Repeat("a", 64)
			intent := func(id string) journal.Intent {
				return journal.Intent{OperationID: id, TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: digest}
			}
			stage, activate, revoke, rollback := intent("stage"), intent("activate"), intent("revoke"), intent("rollback")
			activation := activationFixture(time.Now())
			s := Session{Version: 1, Plan: plan, OldIdentityDigest: RevocationIdentityDigest(identity),
				NewIdentityDigest: strings.Repeat("e", 64), Stage: stage, Activate: &activate, Revoke: &revoke,
				Rollback: &rollback, StageEvidence: activation.StageReceiptDigest, Activation: &activation}
			if scenario == "missing revoke enrollment" {
				s.Revoke = nil
			}
			if _, err := SaveSession(store, SessionSnapshot{}, s); err != nil {
				t.Fatal(err)
			}
			ack := func(op journal.Intent) {
				t.Helper()
				if _, err := store.Dispatch(op, func() (journal.Observation, error) {
					return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			ack(stage)
			ack(activate)
			switch scenario {
			case "revocation acknowledged":
				ack(revoke)
			case "bound same-plan record":
				prior := intent("previous-bound-operation")
				prior.TargetDigest = strings.Repeat("b", 64)
				binding := authoritybinding.Binding{GoalID: "goal", MutationID: prior.OperationID, WriterGeneration: 1,
					Owner: "owner", Session: "session", Provider: "cloudflare", Operation: "registration",
					ResourceDigest: digest, TargetDigest: prior.TargetDigest, PlanDigest: prior.PlanDigest, GrantDigest: prior.GrantDigest}
				if _, err := store.DispatchBound(prior, binding, func(journal.DispatchGuard) (journal.Observation, error) {
					return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
				}); err != nil {
					t.Fatal(err)
				}
			case "legacy same-plan record", "legacy no-effect record":
				legacy := intent("previous-operation")
				legacy.TargetDigest = strings.Repeat("b", 64)
				if scenario == "legacy same-plan record" {
					ack(legacy)
				} else if _, err := store.Dispatch(legacy, func() (journal.Observation, error) {
					return journal.Observation{State: journal.ObservedNoEffect, ProofDigest: digest}, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			reads, calls := 0, 0
			run := func() (journal.Record, error) {
				return Rollback(ctx, store, plan.ID, func(context.Context, string, PreparationPlan) (RollbackGrant, error) {
					return RollbackGrant{PlanDigest: plan.ID, GrantDigest: digest,
						OldCredentialHandle: handle, ExpiresAt: time.Now().Add(time.Minute)}, nil
				}, func(context.Context, PreparationPlan, string) (VerifiedOldCredential, error) {
					reads++
					if scenario == "old generation unavailable" {
						return VerifiedOldCredential{}, errors.New("old credential is unusable")
					}
					return fixtureOldCredential(plan, handle), nil
				}, func(_ context.Context, actual PreparationPlan, got string) (journal.Observation, error) {
					calls++
					if got != handle || actual.ID != plan.ID || actual.Old != plan.Old {
						t.Fatal("restore targeted a different generation")
					}
					other := journal.Store{Root: store.Root}
					if _, err := other.Dispatch(revoke, func() (journal.Observation, error) {
						t.Fatal("revocation entered while rollback held the writer lock")
						return journal.Observation{}, nil
					}); !errors.Is(err, journal.ErrBusy) {
						t.Fatalf("rollback released the writer lock: %v", err)
					}
					return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
				})
			}
			record, err := run()
			if scenario == "missing revoke enrollment" {
				if _, statusErr := store.Status(rollback.OperationID); !errors.Is(err, ErrActionNotCurrent) ||
					!errors.Is(statusErr, journal.ErrNotFound) || reads != 0 || calls != 0 {
					t.Fatalf("missing enrollment reached dispatch: reads=%d calls=%d status=%v error=%v", reads, calls, statusErr, err)
				}
				return
			}
			if scenario != "old generation usable" && scenario != "legacy no-effect record" {
				if !errors.Is(err, ErrActionNotCurrent) || record.State != journal.ObservedNoEffect || calls != 0 ||
					(scenario != "old generation unavailable" && reads != 0) {
					t.Fatalf("unsafe restoration: %+v reads=%d calls=%d error=%v", record, reads, calls, err)
				}
				return
			}
			if err != nil || record.State != journal.Acknowledged || reads != 1 || calls != 1 {
				t.Fatalf("rollback: %+v reads=%d calls=%d error=%v", record, reads, calls, err)
			}
			if _, err := run(); !errors.Is(err, journal.ErrDuplicate) || calls != 1 {
				t.Fatalf("rollback replay called provider: calls=%d error=%v", calls, err)
			}
			if err := CheckRollbackFence(store, revoke); !errors.Is(err, ErrActionNotCurrent) {
				t.Fatalf("restored generation still eligible for revocation: %v", err)
			}
			verify := func(_ context.Context, _ PreparationPlan, got journal.Record, e RestorationEvidence) error {
				if got.Intent != rollback || e.ReceiptDigest != digest {
					return errors.New("unbound restoration")
				}
				return nil
			}
			collect := func(_ context.Context, _ PreparationPlan, _ journal.Record) (RestorationEvidence, error) {
				now := time.Now()
				return RestorationEvidence{ActivatedAt: now.Add(-time.Second), ReceiptDigest: digest, Consumers: []ConsumerEvidence{
					{ConsumerID: "a", Generation: 1, ObservedAt: now, EvidenceDigest: digest},
					{ConsumerID: "b", Generation: 1, ObservedAt: now, EvidenceDigest: digest},
				}}, nil
			}
			if _, err := ConfirmRollback(ctx, store, plan.ID, func(context.Context, PreparationPlan, journal.Record) (RestorationEvidence, error) {
				return RestorationEvidence{}, errors.New("observer disconnected")
			}, verify); !errors.Is(err, ErrActionNotCurrent) {
				t.Fatalf("unconfirmed evidence: %v", err)
			}
			if _, err := ConfirmRollback(ctx, journal.Store{Root: store.Root}, plan.ID, collect, verify); err != nil {
				t.Fatal(err)
			}
			reloaded, err := LoadSession(store, plan.ID)
			if err != nil {
				t.Fatal(err)
			}
			state, err := reloaded.Session.State(ctx, store, time.Now(), SessionVerifiers{Restoration: verify})
			if err != nil || state != SessionActiveVerified || reloaded.Session.Restoration.Consumers[0].Generation != plan.Old.Number || calls != 1 {
				t.Fatalf("reopened restoration: state=%s calls=%d error=%v", state, calls, err)
			}
		})
	}
}
