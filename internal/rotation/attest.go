package rotation

import (
	"context"
	"errors"
	"slices"
	"time"
)

// ActivationEvidence is an untrusted declaration until independently verified
// against the original stage, promotion, provider and trusted host receipts.
type ActivationEvidence struct {
	StageReceiptDigest     string
	PromotionReceiptDigest string
	ActivatedAt            time.Time
	GraceUntil             time.Time
}

// ConsumerEvidence is not a trusted credential proof merely because the shape
// and digest are valid. No evidence token or credential material is stored.
type ConsumerEvidence struct {
	ConsumerID     string
	Generation     uint64
	ObservedAt     time.Time
	EvidenceDigest string
}

type VerifyActivation func(context.Context, PreparationPlan, ActivationEvidence) error
type VerifyConsumer func(context.Context, PreparationPlan, ConsumerEvidence) error

// Attest assembles a pure retirement input only after host verifiers have
// independently enrolled every required consumer plus the stage/promotion.
// The returned decision is always advisory, even if it is READY.
func Attest(ctx context.Context, plan PreparationPlan, activation ActivationEvidence, consumers []ConsumerEvidence, now time.Time, verifyActivation VerifyActivation, verifyConsumer VerifyConsumer) (RetirementInput, RetirementDecision, error) {
	if verifyActivation == nil || verifyConsumer == nil || ctx.Err() != nil || plan.ID == "" || plan.Version != 1 ||
		!validGenerations(plan.Old, plan.New) || preparationDigest(plan) != plan.ID || now.IsZero() ||
		!digestPattern.MatchString(activation.StageReceiptDigest) || !digestPattern.MatchString(activation.PromotionReceiptDigest) ||
		activation.ActivatedAt.IsZero() || !activation.GraceUntil.After(activation.ActivatedAt) {
		return RetirementInput{}, RetirementDecision{}, errors.New("invalid rotation attestation input")
	}
	canonical, err := NewPreparationPlan(plan.Old, plan.New, plan.Consumers)
	if err != nil || canonical.ID != plan.ID || !slices.EqualFunc(canonical.Consumers, plan.Consumers, func(a, b Consumer) bool { return a == b }) {
		return RetirementInput{}, RetirementDecision{}, errors.New("noncanonical credential consumers")
	}
	if err := verifyActivation(ctx, plan, activation); err != nil {
		return RetirementInput{}, RetirementDecision{}, errors.New("trusted activation evidence unavailable")
	}
	if len(consumers) != len(plan.Consumers) {
		return RetirementInput{}, RetirementDecision{}, errors.New("missing or extra consumer attestation")
	}
	seen := map[string]bool{}
	obs := make([]ConsumerObservation, 0, len(consumers))
	for _, e := range consumers {
		if seen[e.ConsumerID] || e.Generation != plan.New.Number || !digestPattern.MatchString(e.EvidenceDigest) ||
			e.ObservedAt.Before(activation.ActivatedAt) || e.ObservedAt.After(now) || now.Sub(e.ObservedAt) > 15*time.Minute {
			return RetirementInput{}, RetirementDecision{}, errors.New("stale or duplicate consumer attestation")
		}
		if !slices.ContainsFunc(plan.Consumers, func(c Consumer) bool { return c.ID == e.ConsumerID && c.Required }) {
			return RetirementInput{}, RetirementDecision{}, errors.New("consumer outside approved rotation plan")
		}
		if err := verifyConsumer(ctx, plan, e); err != nil {
			return RetirementInput{}, RetirementDecision{}, errors.New("consumer evidence was not verified by the host")
		}
		seen[e.ConsumerID] = true
		obs = append(obs, ConsumerObservation{ID: e.ConsumerID, Generation: e.Generation, SeenAt: e.ObservedAt, EvidenceDigest: e.EvidenceDigest, TrustedByHost: true})
	}
	slices.SortFunc(obs, func(a, b ConsumerObservation) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	input := RetirementInput{
		Old: plan.Old, New: plan.New, Consumers: slices.Clone(plan.Consumers), Observations: obs,
		StageVerified: true, PromotionVerified: true, SourceAndTargetBound: true,
		NewActivatedAt: activation.ActivatedAt, GraceUntil: activation.GraceUntil, Now: now,
	}
	decision, err := EvaluateRetirement(input)
	if err != nil {
		return RetirementInput{}, RetirementDecision{}, err
	}
	return input, decision, nil
}
