package rotation

import (
	"strings"
	"testing"
	"time"
)

func fixture() RetirementInput {
	now := time.Date(2026, 11, 10, 4, 0, 0, 0, time.UTC)
	old := Generation{Alias: "openai_worker", Kind: StaticServiceKey, Provider: "cloudflare", Target: "worker-staging", Number: 1}
	next := old
	next.Number = 2
	return RetirementInput{
		Old: old, New: next, Consumers: []Consumer{{ID: "worker-runtime", Required: true}, {ID: "test-agent", Required: true}},
		Observations: []ConsumerObservation{
			{ID: "worker-runtime", Generation: 2, SeenAt: now.Add(-time.Minute), EvidenceDigest: strings.Repeat("a", 64), TrustedByHost: true},
			{ID: "test-agent", Generation: 2, SeenAt: now.Add(-time.Minute), EvidenceDigest: strings.Repeat("b", 64), TrustedByHost: true},
		},
		StageVerified: true, PromotionVerified: true, SourceAndTargetBound: true, NewActivatedAt: now.Add(-time.Hour),
		GraceUntil: now.Add(-time.Minute), Now: now,
	}
}
func TestCandidateNeverGrantsRevocation(t *testing.T) {
	d, err := EvaluateRetirement(fixture())
	if err != nil || d.Status != RetirementCandidate || d.AuthorizesRevocation {
		t.Fatalf("unexpected: %+v %v", d, err)
	}
}
func TestOutstandingGraceBlocks(t *testing.T) {
	in := fixture()
	in.GraceUntil = in.Now.Add(time.Minute)
	d, err := EvaluateRetirement(in)
	if err != nil || d.Status != RetirementHold {
		t.Fatal(d, err)
	}
}
func TestOldConsumerAndRemoteUnknownBlock(t *testing.T) {
	in := fixture()
	in.Observations[1].Generation = 1
	d, err := EvaluateRetirement(in)
	if err != nil || d.Status != RetirementHold {
		t.Fatal(d, err)
	}
	in = fixture()
	in.UnknownRemoteEffects = true
	d, err = EvaluateRetirement(in)
	if err != nil || d.Status != RetirementHold {
		t.Fatal(d, err)
	}
}
func TestWIFAndDuplicateConsumersReject(t *testing.T) {
	in := fixture()
	in.New.Kind = "wif"
	if _, err := EvaluateRetirement(in); err == nil {
		t.Fatal("WIF rotated as static key")
	}
	in = fixture()
	in.Consumers = append(in.Consumers, in.Consumers[0])
	if _, err := EvaluateRetirement(in); err == nil {
		t.Fatal("duplicate consumer")
	}
}
func TestMissingOrStaleProofBlocks(t *testing.T) {
	in := fixture()
	in.Observations = in.Observations[:1]
	d, err := EvaluateRetirement(in)
	if err != nil || d.Status != RetirementHold {
		t.Fatal(d, err)
	}
	in = fixture()
	in.Observations[0].SeenAt = in.Now.Add(-16 * time.Minute)
	d, err = EvaluateRetirement(in)
	if err != nil || d.Status != RetirementHold {
		t.Fatal(d, err)
	}
}
