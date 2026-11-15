package rotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

func TestFinalizeReverifiesAndRequiresDistinctGrant(t *testing.T) {
	t.Run("valid at dispatch", func(t *testing.T) { testFinalizeReverification(t, false) })
	t.Run("consumer proof withdrawn", func(t *testing.T) { testFinalizeReverification(t, true) })
}

func testFinalizeReverification(t *testing.T, withdrawn bool) {
	t.Helper()
	p := planFixture(t)
	now := time.Now()
	activation := activationFixture(now)
	consumers := evidenceFixture(now)
	b, _ := json.Marshal(struct {
		Activation ActivationEvidence
		Consumers  []ConsumerEvidence
	}{activation, consumers})
	sum := sha256.Sum256(b)
	digest := hex.EncodeToString(sum[:])
	called := 0
	consumerVerifies := 0
	result, err := Finalize(context.Background(), rotationStore(t), "revoke-old", p, activation, consumers, now, verifyActivationOK,
		func(context.Context, PreparationPlan, ConsumerEvidence) error {
			consumerVerifies++
			if withdrawn && consumerVerifies > len(consumers) {
				return ErrActionNotCurrent
			}
			return nil
		},
		func(_ context.Context, id string, p PreparationPlan, d RetirementDecision) (RevocationGrant, error) {
			if d.Status != RetirementCandidate || d.AuthorizesRevocation {
				t.Fatal("unsafe decision")
			}
			return RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("e", 64), OldCredentialHandle: cloudflareOldHandle(), OldIdentityDigest: RevocationIdentityDigest(fixtureOldCredential(p, cloudflareOldHandle())), EvidenceDigest: digest, ExpiresAt: time.Now().Add(time.Minute)}, nil
		}, fixtureIdentityVerifier, func(ctx context.Context, old Generation, handle string) (journal.Observation, error) {
			called++
			if handle != cloudflareOldHandle() || old.Number != 1 {
				t.Fatal("wrong old target")
			}
			return journal.Observation{State: journal.Acknowledged, ProofDigest: strings.Repeat("f", 64)}, nil
		})
	if withdrawn {
		if !errors.Is(err, ErrActionNotCurrent) || result.State != journal.ObservedNoEffect || called != 0 || consumerVerifies != len(consumers)+1 {
			t.Fatalf("withdrawn consumer proof: %+v, calls=%d, verifies=%d, error=%v", result, called, consumerVerifies, err)
		}
		return
	}
	if err != nil || result.State != journal.Acknowledged || called != 1 || consumerVerifies != 2*len(consumers) {
		t.Fatalf("unexpected finalization %+v %v", result, err)
	}
}

func TestFinalizeRejectsForgedGrantBeforeEffect(t *testing.T) {
	p := planFixture(t)
	now := time.Now()
	called := false
	_, err := Finalize(context.Background(), rotationStore(t), "rev", p, activationFixture(now), evidenceFixture(now), now, verifyActivationOK, verifyConsumerOK,
		func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error) {
			return RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("e", 64), OldCredentialHandle: cloudflareOldHandle(), OldIdentityDigest: RevocationIdentityDigest(fixtureOldCredential(p, cloudflareOldHandle())), EvidenceDigest: "invalid", ExpiresAt: time.Now().Add(time.Minute)}, nil
		},
		fixtureIdentityVerifier,
		func(context.Context, Generation, string) (journal.Observation, error) {
			called = true
			return journal.Observation{}, nil
		})
	if err == nil || called {
		t.Fatal("forged high-risk grant used")
	}
}

func TestFinalizeRejectsMismatchedRevokeEnrollmentBeforeDispatch(t *testing.T) {
	ctx := context.Background()
	store, plan, now := rotationStore(t), planFixture(t), time.Now()
	grantDigest := strings.Repeat("e", 64)
	handle := cloudflareOldHandle()
	identity := RevocationIdentityDigest(fixtureOldCredential(plan, handle))
	stage := journal.Intent{OperationID: "stage", TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: grantDigest}
	revoke := stage
	revoke.OperationID = "enrolled"
	if _, err := SaveSession(store, SessionSnapshot{}, Session{
		Version: 1, Plan: plan, OldIdentityDigest: identity, Stage: stage, Revoke: &revoke,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := Finalize(ctx, store, "different-revoke", plan, activationFixture(now), evidenceFixture(now), now,
		verifyActivationOK, verifyConsumerOK,
		func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error) {
			return RevocationGrant{PlanDigest: plan.ID, GrantDigest: grantDigest, OldCredentialHandle: handle,
				OldIdentityDigest: identity, PreapprovedOperation: "different-revoke", MinimumGrace: time.Minute,
				ExpiresAt: now.Add(time.Minute)}, nil
		}, func(context.Context, PreparationPlan, string) (VerifiedOldCredential, error) {
			t.Fatal("mismatched enrollment reached identity verification")
			return VerifiedOldCredential{}, nil
		}, func(context.Context, Generation, string) (journal.Observation, error) {
			t.Fatal("mismatched enrollment reached revocation")
			return journal.Observation{}, nil
		})
	if _, statusErr := store.Status("different-revoke"); !errors.Is(err, ErrActionNotCurrent) || !errors.Is(statusErr, journal.ErrNotFound) {
		t.Fatalf("mismatched enrollment was dispatched: status=%v error=%v", statusErr, err)
	}
}

func TestFinalizeUnknownRevokeMustNotAutoRetry(t *testing.T) {
	p := planFixture(t)
	now := time.Now()
	activation := activationFixture(now)
	consumers := evidenceFixture(now)
	b, _ := json.Marshal(struct {
		Activation ActivationEvidence
		Consumers  []ConsumerEvidence
	}{activation, consumers})
	sum := sha256.Sum256(b)
	result, err := Finalize(context.Background(), rotationStore(t), "rev", p, activation, consumers, now, verifyActivationOK, verifyConsumerOK,
		func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error) {
			return RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("e", 64), OldCredentialHandle: cloudflareOldHandle(), OldIdentityDigest: RevocationIdentityDigest(fixtureOldCredential(p, cloudflareOldHandle())), EvidenceDigest: hex.EncodeToString(sum[:]), ExpiresAt: time.Now().Add(time.Minute)}, nil
		},
		fixtureIdentityVerifier,
		func(context.Context, Generation, string) (journal.Observation, error) {
			return journal.Observation{}, errors.New("provider response missing")
		})
	if !errors.Is(err, journal.ErrUnknownEffect) || result.State != journal.UnknownEffect {
		t.Fatalf("unknown revoke erased: %+v %v", result, err)
	}
}

func TestPreapprovedFinalizeAcceptsFreshEvidenceWithinScope(t *testing.T) {
	plan := planFixture(t)
	now := time.Now()
	activation := activationFixture(now)
	grant := RevocationGrant{
		PlanDigest: plan.ID, GrantDigest: strings.Repeat("e", 64),
		OldCredentialHandle:  cloudflareOldHandle(),
		OldIdentityDigest:    RevocationIdentityDigest(fixtureOldCredential(plan, cloudflareOldHandle())),
		PreapprovedOperation: "revoke-old", MinimumGrace: 30 * time.Minute, ExpiresAt: now.Add(time.Minute),
	}
	for _, tc := range []struct {
		name, operation string
		grace           time.Duration
		stale           bool
		wantCalls       int
	}{
		{"fresh consumer proof", "revoke-old", 30 * time.Minute, false, 1},
		{"another operation", "revoke-other", 30 * time.Minute, false, 0},
		{"shortened grace", "revoke-old", 29 * time.Minute, false, 0},
		{"stale consumer proof", "revoke-old", 30 * time.Minute, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			evidence := evidenceFixture(now)
			if tc.stale {
				evidence[0].ObservedAt = now.Add(-16 * time.Minute)
			}
			approval := activation
			approval.GraceUntil = approval.ActivatedAt.Add(tc.grace)
			calls := 0
			_, err := Finalize(context.Background(), rotationStore(t), tc.operation, plan, approval, evidence, now,
				verifyActivationOK, verifyConsumerOK,
				func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error) {
					return grant, nil
				}, fixtureIdentityVerifier,
				func(context.Context, Generation, string) (journal.Observation, error) {
					calls++
					return journal.Observation{State: journal.Acknowledged, ProofDigest: strings.Repeat("f", 64)}, nil
				})
			if calls != tc.wantCalls || (err == nil) != (tc.wantCalls > 0) {
				t.Fatalf("calls=%d error=%v; wanted %d calls", calls, err, tc.wantCalls)
			}
		})
	}
}
