package rotation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func activationFixture(now time.Time) ActivationEvidence {
	return ActivationEvidence{StageReceiptDigest: strings.Repeat("a", 64), PromotionReceiptDigest: strings.Repeat("b", 64), ActivatedAt: now.Add(-time.Hour), GraceUntil: now.Add(-time.Minute)}
}

func evidenceFixture(now time.Time) []ConsumerEvidence {
	return []ConsumerEvidence{{ConsumerID: "a", Generation: 2, ObservedAt: now.Add(-time.Second), EvidenceDigest: strings.Repeat("c", 64)},
		{ConsumerID: "b", Generation: 2, ObservedAt: now.Add(-time.Second), EvidenceDigest: strings.Repeat("d", 64)}}
}

func verifyActivationOK(context.Context, PreparationPlan, ActivationEvidence) error { return nil }

func verifyConsumerOK(context.Context, PreparationPlan, ConsumerEvidence) error { return nil }

func TestAttestRequiresAllIndependentConsumerEvidence(t *testing.T) {
	p := planFixture(t)
	now := time.Now()
	activation := activationFixture(now)
	proofs := evidenceFixture(now)
	input, decision, err := Attest(context.Background(), p, activation, proofs, now, verifyActivationOK, verifyConsumerOK)
	if err != nil || decision.Status != RetirementCandidate || decision.AuthorizesRevocation || len(input.Observations) != 2 {
		t.Fatalf("bad attestation %+v %v", decision, err)
	}
	_, _, err = Attest(context.Background(), p, activation, proofs, now, func(context.Context, PreparationPlan, ActivationEvidence) error { return errors.New("not enrolled") }, verifyConsumerOK)
	if err == nil {
		t.Fatal("untrusted activation accepted")
	}
	proofs[0].Generation = 1
	_, _, err = Attest(context.Background(), p, activation, proofs, now, verifyActivationOK, verifyConsumerOK)
	if err == nil {
		t.Fatal("old generation accepted")
	}
}

func TestAttestHoldsDuringGraceAndStaleProof(t *testing.T) {
	p := planFixture(t)
	now := time.Now()
	activation := activationFixture(now)
	proofs := evidenceFixture(now)
	activation.GraceUntil = now.Add(time.Minute)
	_, d, err := Attest(context.Background(), p, activation, proofs, now, verifyActivationOK, verifyConsumerOK)
	if err != nil || d.Status != RetirementHold {
		t.Fatal("grace not held")
	}
	activation = activationFixture(now)
	proofs[0].ObservedAt = now.Add(-16 * time.Minute)
	_, _, err = Attest(context.Background(), p, activation, proofs, now, verifyActivationOK, verifyConsumerOK)
	if err == nil {
		t.Fatal("old proof accepted")
	}
}
