package rotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

// RevocationGrant must be issued by a separately privileged and authenticated
// host. A prepared-generation writer cannot revoke by reusing its stage grant.
type RevocationGrant struct {
	PlanDigest          string
	GrantDigest         string
	OldCredentialHandle string // provider-specific immutable identifier, see VerifyOldCredential
	OldIdentityDigest   string // independently observed provider identity bound to this grant
	EvidenceDigest      string // binds the exact current verified consumer/activation evidence
	ExpiresAt           time.Time
}

type AuthorizeRevocation func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error)
type RevokeOldGeneration func(context.Context, Generation, string) (journal.Observation, error)

// FinalizeAction binds a separately authorized revocation to its exact plan,
// retirement evidence and old credential. The caller owns journal dispatch.
type FinalizeAction struct {
	intent     journal.Intent
	plan       PreparationPlan
	retirement RetirementInput
	grant      RevocationGrant
	verifyOld  VerifyOldCredential
	revoke     RevokeOldGeneration
}

// Finalize attests activation, every required consumer and the old credential
// before invoking a separately authorized revocation inside the durable journal.
// A successful provider acknowledgement is NOT proof of revocation completion.
func Finalize(ctx context.Context, store journal.Store, operationID string, plan PreparationPlan,
	activation ActivationEvidence, consumers []ConsumerEvidence, now time.Time,
	verifyActivation VerifyActivation, verifyConsumer VerifyConsumer,
	authorizeRevoker AuthorizeRevocation, verifyOld VerifyOldCredential, revoke RevokeOldGeneration) (journal.Record, error) {
	action, err := NewFinalizeAction(ctx, operationID, plan, activation, consumers, now, verifyActivation, verifyConsumer, authorizeRevoker, verifyOld, revoke)
	if err != nil {
		return journal.Record{}, err
	}
	ctx, cancel := context.WithDeadline(ctx, action.grant.ExpiresAt)
	defer cancel()
	// Preserve the direct caller's early rejection of an unverifiable identity.
	identity, err := action.readIdentity(ctx)
	if err != nil {
		return journal.Record{}, errors.New("old credential provider identity not verified")
	}
	return dispatchAction(store, action.Intent(), func() (journal.Observation, error) {
		return action.revokeVerified(ctx, identity, nil)
	})
}

// NewFinalizeAction validates retirement evidence and admits the revocation.
// Execute independently reads the old credential identity at dispatch time.
func NewFinalizeAction(ctx context.Context, operationID string, plan PreparationPlan,
	activation ActivationEvidence, consumers []ConsumerEvidence, now time.Time,
	verifyActivation VerifyActivation, verifyConsumer VerifyConsumer,
	authorizeRevoker AuthorizeRevocation, verifyOld VerifyOldCredential, revoke RevokeOldGeneration) (FinalizeAction, error) {
	wall := time.Now()
	if authorizeRevoker == nil || verifyOld == nil || revoke == nil || operationID == "" || ctx == nil || ctx.Err() != nil || now.Before(wall.Add(-30*time.Second)) || now.After(wall.Add(30*time.Second)) {
		return FinalizeAction{}, errors.New("credential revocation host authorization missing")
	}
	plan = clonePreparationPlan(plan)
	consumers = slices.Clone(consumers)
	retirement, decision, err := Attest(ctx, clonePreparationPlan(plan), activation, consumers, now, verifyActivation, verifyConsumer)
	if err != nil || decision.Status != RetirementCandidate || decision.AuthorizesRevocation {
		return FinalizeAction{}, errors.New("credential generation not eligible for revocation")
	}
	evidence, encodeErr := json.Marshal(struct {
		Activation ActivationEvidence
		Consumers  []ConsumerEvidence
	}{activation, consumers})
	if encodeErr != nil {
		return FinalizeAction{}, errors.New("cannot bind revocation evidence")
	}
	sum := sha256.Sum256(evidence)
	proofDigest := hex.EncodeToString(sum[:])
	grant, err := authorizeRevoker(ctx, operationID, clonePreparationPlan(plan), decision)
	if err != nil || grant.PlanDigest != plan.ID || !digestPattern.MatchString(grant.GrantDigest) ||
		!namePattern.MatchString(grant.OldCredentialHandle) || grant.EvidenceDigest != proofDigest || !digestPattern.MatchString(grant.OldIdentityDigest) || !time.Now().Before(grant.ExpiresAt) || ctx.Err() != nil {
		return FinalizeAction{}, errors.New("separate provider revocation grant not admitted")
	}
	intent := journal.Intent{OperationID: operationID, TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: grant.GrantDigest}
	return FinalizeAction{intent: intent, plan: plan, retirement: retirement, grant: grant, verifyOld: verifyOld, revoke: revoke}, nil
}

func (a FinalizeAction) Intent() journal.Intent { return a.intent }

// Plan returns a copy of the admitted metadata for trusted target binding.
func (a FinalizeAction) Plan() PreparationPlan { return clonePreparationPlan(a.plan) }

// Execute reads the exact old identity and invokes the caller's execution fence
// before attempting revocation.
func (a FinalizeAction) Execute(ctx context.Context, beforeEffect func(context.Context) error) (journal.Observation, error) {
	if a.verifyOld == nil || !actionCurrent(ctx, a.grant.ExpiresAt) {
		return journal.Observation{}, ErrActionNotCurrent
	}
	ctx, cancel := context.WithDeadline(ctx, a.grant.ExpiresAt)
	defer cancel()
	identity, err := a.readIdentity(ctx)
	if err != nil {
		return journal.Observation{}, err
	}
	return a.revokeVerified(ctx, identity, beforeEffect)
}

func (a FinalizeAction) readIdentity(ctx context.Context) (VerifiedOldCredential, error) {
	// An opaque handle echoed by the revoker grant is not a provider identity.
	identity, err := a.verifyOld(ctx, clonePreparationPlan(a.plan), a.grant.OldCredentialHandle)
	if err != nil || validOldCredentialIdentity(a.plan, a.grant.OldCredentialHandle, identity, time.Now()) != nil ||
		revocationIdentityDigest(identity) != a.grant.OldIdentityDigest || !actionCurrent(ctx, a.grant.ExpiresAt) {
		return VerifiedOldCredential{}, fmt.Errorf("old credential provider identity not verified: %w", ErrActionNotCurrent)
	}
	return identity, nil
}

func (a FinalizeAction) revokeVerified(ctx context.Context, identity VerifiedOldCredential, beforeEffect func(context.Context) error) (journal.Observation, error) {
	if !a.evidenceCurrent(identity) {
		return journal.Observation{}, ErrActionNotCurrent
	}
	if err := verifyActionBoundary(ctx, a.grant.ExpiresAt, beforeEffect); err != nil {
		return journal.Observation{}, err
	}
	if beforeEffect != nil && !a.evidenceCurrent(identity) {
		return journal.Observation{}, ErrActionNotCurrent
	}
	return acknowledged(a.revoke(ctx, a.plan.Old, a.grant.OldCredentialHandle))
}

func (a FinalizeAction) evidenceCurrent(identity VerifiedOldCredential) bool {
	retirement := a.retirement
	retirement.Now = time.Now()
	decision, err := EvaluateRetirement(retirement)
	return err == nil && decision.Status == RetirementCandidate &&
		validOldCredentialIdentity(a.plan, a.grant.OldCredentialHandle, identity, retirement.Now) == nil
}
