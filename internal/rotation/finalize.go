package rotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// Finalize re-attests activation and every required consumer immediately before
// invoking a single separately authorized revocation inside the durable journal.
// A successful provider acknowledgement is NOT proof of revocation completion.
func Finalize(ctx context.Context, store journal.Store, operationID string, plan PreparationPlan,
	activation ActivationEvidence, consumers []ConsumerEvidence, now time.Time,
	verifyActivation VerifyActivation, verifyConsumer VerifyConsumer,
	authorizeRevoker AuthorizeRevocation, verifyOld VerifyOldCredential, revoke RevokeOldGeneration) (journal.Record, error) {
	if authorizeRevoker == nil || verifyOld == nil || revoke == nil || operationID == "" || ctx.Err() != nil || now.Before(time.Now().Add(-30*time.Second)) || now.After(time.Now().Add(30*time.Second)) {
		return journal.Record{}, errors.New("credential revocation host authorization missing")
	}
	_, decision, err := Attest(ctx, plan, activation, consumers, now, verifyActivation, verifyConsumer)
	if err != nil || decision.Status != RetirementCandidate || decision.AuthorizesRevocation {
		return journal.Record{}, errors.New("credential generation not eligible for revocation")
	}
	evidence, encodeErr := json.Marshal(struct {
		Activation ActivationEvidence
		Consumers  []ConsumerEvidence
	}{activation, consumers})
	if encodeErr != nil {
		return journal.Record{}, errors.New("cannot bind revocation evidence")
	}
	sum := sha256.Sum256(evidence)
	proofDigest := hex.EncodeToString(sum[:])
	grant, err := authorizeRevoker(ctx, operationID, plan, decision)
	if err != nil || grant.PlanDigest != plan.ID || !digestPattern.MatchString(grant.GrantDigest) ||
		!namePattern.MatchString(grant.OldCredentialHandle) || grant.EvidenceDigest != proofDigest || !digestPattern.MatchString(grant.OldIdentityDigest) || !time.Now().Before(grant.ExpiresAt) || ctx.Err() != nil {
		return journal.Record{}, errors.New("separate provider revocation grant not admitted")
	}
	ctx, cancel := context.WithDeadline(ctx, grant.ExpiresAt)
	defer cancel()
	// An opaque handle echoed by the revoker grant is not a provider identity.
	// The independently trusted host must verify the *actual old* resource ID
	// and its provider/account/secret/version before any journal effect.
	identity, err := verifyOld(ctx, plan, grant.OldCredentialHandle)
	if err != nil || validOldCredentialIdentity(plan, grant.OldCredentialHandle, identity, time.Now()) != nil ||
		revocationIdentityDigest(identity) != grant.OldIdentityDigest || ctx.Err() != nil {
		return journal.Record{}, errors.New("old credential provider identity not verified")
	}
	intent := journal.Intent{OperationID: operationID, TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: grant.GrantDigest}
	return store.Dispatch(intent, func() (journal.Observation, error) {
		obs, err := revoke(ctx, plan.Old, grant.OldCredentialHandle)
		if err != nil || obs.State != journal.Acknowledged || !digestPattern.MatchString(obs.ProofDigest) {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		return obs, nil
	})
}
