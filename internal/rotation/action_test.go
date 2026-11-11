package rotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

func TestPrepareActionUsesCallerDispatch(t *testing.T) {
	ctx := context.Background()
	store := rotationStore(t)
	plan := planFixture(t)
	digest := strings.Repeat("a", 64)
	var calls []string
	action, err := NewPrepareAction(ctx, "stage-action", plan,
		func(context.Context, string, PreparationPlan) (PreparationGrant, error) {
			return PreparationGrant{PlanDigest: plan.ID, GrantDigest: digest, ExpiresAt: time.Now().Add(time.Minute)}, nil
		},
		func(_ context.Context, p PreparationPlan) (journal.Observation, error) {
			calls = append(calls, "stage")
			if p.Consumers[0].ID != "a" {
				t.Fatal("admitted plan mutated")
			}
			return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
		})
	if err != nil || len(calls) != 0 {
		t.Fatalf("construction produced an effect: %v %v", calls, err)
	}
	plan.Consumers[0].ID = "changed"
	action.Plan().Consumers[0].ID = "also-changed"
	execute := func() (journal.Observation, error) {
		return action.Execute(ctx, func(context.Context) error {
			calls = append(calls, "fence")
			return nil
		})
	}
	record, err := store.Dispatch(action.Intent(), execute)
	if err != nil || record.State != journal.Acknowledged {
		t.Fatalf("outer dispatch failed: %+v %v", record, err)
	}
	if _, err := store.Dispatch(action.Intent(), execute); !errors.Is(err, journal.ErrDuplicate) {
		t.Fatalf("duplicate dispatch: %v", err)
	}
	if !slices.Equal(calls, []string{"fence", "stage"}) {
		t.Fatalf("unexpected calls: %v", calls)
	}
	action.expiresAt = time.Now().Add(-time.Second)
	if _, err := execute(); !errors.Is(err, ErrActionNotCurrent) || len(calls) != 2 {
		t.Fatalf("expired action reached the fence or provider: %v %v", calls, err)
	}
}

func TestFinalizeActionUsesCallerDispatch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		drift    bool
		deny     bool
		wantCall []string
	}{
		{name: "admitted", wantCall: []string{"identity", "fence", "revoke"}},
		{name: "identity drift", drift: true, wantCall: []string{"identity"}},
		{name: "fence rejects", deny: true, wantCall: []string{"identity", "fence"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := rotationStore(t)
			plan := planFixture(t)
			now := time.Now()
			activation := activationFixture(now)
			consumers := evidenceFixture(now)
			evidence, err := json.Marshal(struct {
				Activation ActivationEvidence
				Consumers  []ConsumerEvidence
			}{activation, consumers})
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(evidence)
			handle := cloudflareOldHandle()
			digest := strings.Repeat("d", 64)
			var calls []string
			action, err := NewFinalizeAction(ctx, "finalize-action", plan, activation, consumers, now, verifyActivationOK, verifyConsumerOK,
				func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error) {
					return RevocationGrant{PlanDigest: plan.ID, GrantDigest: digest, OldCredentialHandle: handle,
						OldIdentityDigest: revocationIdentityDigest(fixtureOldCredential(plan, handle)),
						EvidenceDigest:    hex.EncodeToString(sum[:]), ExpiresAt: time.Now().Add(time.Minute)}, nil
				},
				func(_ context.Context, p PreparationPlan, handle string) (VerifiedOldCredential, error) {
					calls = append(calls, "identity")
					identity := fixtureOldCredential(p, handle)
					if tc.drift {
						identity.ProofDigest = strings.Repeat("e", 64)
					}
					return identity, nil
				},
				func(context.Context, Generation, string) (journal.Observation, error) {
					calls = append(calls, "revoke")
					return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
				})
			if err != nil || len(calls) != 0 {
				t.Fatalf("construction touched the provider: %v %v", calls, err)
			}
			denied := errors.New("execution fence rejected action")
			var actionErr error
			execute := func() (journal.Observation, error) {
				observation, err := action.Execute(ctx, func(context.Context) error {
					calls = append(calls, "fence")
					if tc.deny {
						return denied
					}
					return nil
				})
				actionErr = err
				return observation, err
			}
			record, err := store.Dispatch(action.Intent(), execute)
			switch {
			case tc.drift:
				if !errors.Is(actionErr, ErrActionNotCurrent) {
					t.Fatalf("identity drift: %v", actionErr)
				}
			case tc.deny:
				if !errors.Is(actionErr, denied) {
					t.Fatalf("fence error lost: %v", actionErr)
				}
			default:
				if err != nil || record.State != journal.Acknowledged {
					t.Fatalf("outer dispatch failed: %+v %v", record, err)
				}
				if _, err := store.Dispatch(action.Intent(), execute); !errors.Is(err, journal.ErrDuplicate) {
					t.Fatalf("duplicate dispatch: %v", err)
				}
			}
			if !slices.Equal(calls, tc.wantCall) {
				t.Fatalf("unexpected calls: %v", calls)
			}
		})
	}
}
