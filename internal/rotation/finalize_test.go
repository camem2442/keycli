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
		func(ctx context.Context, p PreparationPlan, e ConsumerEvidence) error { consumerVerifies++; return nil },
		func(_ context.Context, id string, p PreparationPlan, d RetirementDecision) (RevocationGrant, error) {
			if d.Status != RetirementCandidate || d.AuthorizesRevocation {
				t.Fatal("unsafe decision")
			}
			return RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("e", 64), OldCredentialHandle: cloudflareOldHandle(), OldIdentityDigest: revocationIdentityDigest(fixtureOldCredential(p, cloudflareOldHandle())), EvidenceDigest: digest, ExpiresAt: time.Now().Add(time.Minute)}, nil
		}, fixtureIdentityVerifier, func(ctx context.Context, old Generation, handle string) (journal.Observation, error) {
			called++
			if handle != cloudflareOldHandle() || old.Number != 1 {
				t.Fatal("wrong old target")
			}
			return journal.Observation{State: journal.Acknowledged, ProofDigest: strings.Repeat("f", 64)}, nil
		})
	if err != nil || result.State != journal.Acknowledged || called != 1 || consumerVerifies != 2 {
		t.Fatalf("unexpected finalization %+v %v", result, err)
	}
}

func TestFinalizeRejectsForgedGrantBeforeEffect(t *testing.T) {
	p := planFixture(t)
	now := time.Now()
	called := false
	_, err := Finalize(context.Background(), rotationStore(t), "rev", p, activationFixture(now), evidenceFixture(now), now, verifyActivationOK, verifyConsumerOK,
		func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error) {
			return RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("e", 64), OldCredentialHandle: cloudflareOldHandle(), OldIdentityDigest: revocationIdentityDigest(fixtureOldCredential(p, cloudflareOldHandle())), EvidenceDigest: "invalid", ExpiresAt: time.Now().Add(time.Minute)}, nil
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
			return RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("e", 64), OldCredentialHandle: cloudflareOldHandle(), OldIdentityDigest: revocationIdentityDigest(fixtureOldCredential(p, cloudflareOldHandle())), EvidenceDigest: hex.EncodeToString(sum[:]), ExpiresAt: time.Now().Add(time.Minute)}, nil
		},
		fixtureIdentityVerifier,
		func(context.Context, Generation, string) (journal.Observation, error) {
			return journal.Observation{}, errors.New("provider response missing")
		})
	if !errors.Is(err, journal.ErrUnknownEffect) || result.State != journal.UnknownEffect {
		t.Fatalf("unknown revoke erased: %+v %v", result, err)
	}
}
