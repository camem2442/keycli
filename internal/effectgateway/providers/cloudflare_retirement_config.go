package providers

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/json"
	"regexp"
	"slices"
	"time"

	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
)

const retirementEvidenceDomain = "keycli/cloudflare-retirement-evidence/v1\x00"

var retirementDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

type retirementEvidence struct {
	activation rotation.ActivationEvidence
	consumers  []rotation.ConsumerEvidence
}

func readRetirementEvidence(path string, key ed25519.PublicKey, registration effectgateway.RegisteredOperation) (retirementEvidence, error) {
	var payload []byte
	var signature []byte
	if err := effectgateway.ReadPrivateConfig(path, map[string]any{"payload": &payload, "signature": &signature}); err != nil ||
		!ed25519.Verify(key, append([]byte(retirementEvidenceDomain), payload...), signature) {
		return retirementEvidence{}, errProductConfig
	}
	var evidence retirementEvidence
	var operation, mutation, plan, target string
	if !effectgateway.DecodePrivateConfig(payload, map[string]any{
		"operation_id": &operation, "mutation_id": &mutation, "plan_digest": &plan, "target_digest": &target,
		"activation": &evidence.activation, "consumers": &evidence.consumers,
	}) || operation != registration.Identity.OperationID || mutation != registration.Identity.MutationID || plan != registration.PlanDigest || target != registration.Binding().TargetDigest {
		return retirementEvidence{}, errProductConfig
	}
	return evidence, nil
}

func loadRetirement(raw json.RawMessage, registration effectgateway.RegisteredOperation, reader provideridentity.Reader, issuerToken, readerToken string) (productOperation, error) {
	var goal, mutation, account, alias, oldID, oldName, issuedOn, policyDigest, oldIdentity string
	var accountReaderFile, revokerFile, evidenceFile string
	var old, next uint64
	var graceSeconds int64
	var expiry time.Time
	var consumers []string
	var signer []byte
	if !effectgateway.DecodePrivateConfig(raw, map[string]any{
		"goal_id": &goal, "mutation_id": &mutation, "account_id": &account, "credential_alias": &alias,
		"old_generation": &old, "new_generation": &next, "required_consumers": &consumers,
		"old_token_id": &oldID, "old_token_name": &oldName, "old_token_issued_on": &issuedOn,
		"old_policy_digest": &policyDigest, "old_identity_digest": &oldIdentity,
		"account_reader_file": &accountReaderFile, "revoker_file": &revokerFile,
		"evidence_file": &evidenceFile, "evidence_public_key": &signer,
		"minimum_grace_seconds": &graceSeconds, "grant_expires_at": &expiry,
	}) || !cloudflareID(account) || !cloudflareID(oldID) || !issueName.MatchString(oldName) ||
		!retirementDigest.MatchString(policyDigest) || !retirementDigest.MatchString(oldIdentity) ||
		len(signer) != ed25519.PublicKeySize || graceSeconds < 1 || graceSeconds > int64((1<<63-1)/time.Second) ||
		expiry.IsZero() || registration.Provider != "cloudflare" ||
		registration.CanonicalTarget != account+"/tokens/"+oldID {
		return productOperation{}, errProductConfig
	}
	binding := registration.Binding()
	if !binding.Valid() || binding.GoalID != goal || binding.MutationID != mutation {
		return productOperation{}, errProductConfig
	}
	oldGeneration := rotation.Generation{Alias: alias, Kind: rotation.StaticServiceKey, Provider: "cloudflare", Target: account, Number: old}
	newGeneration := oldGeneration
	newGeneration.Number = next
	required := make([]rotation.Consumer, 0, len(consumers))
	for _, consumer := range consumers {
		required = append(required, rotation.Consumer{ID: consumer, Required: true})
	}
	plan, err := rotation.NewPreparationPlan(oldGeneration, newGeneration, required)
	if err != nil || plan.ID != binding.PlanDigest {
		return productOperation{}, errProductConfig
	}
	accountToken, err := loadBearer(accountReaderFile)
	if err != nil {
		return productOperation{}, errProductConfig
	}
	revokerToken, err := loadBearer(revokerFile)
	if err != nil || subtle.ConstantTimeCompare([]byte(revokerToken), []byte(issuerToken)) == 1 ||
		subtle.ConstantTimeCompare([]byte(revokerToken), []byte(readerToken)) == 1 ||
		subtle.ConstantTimeCompare([]byte(revokerToken), []byte(accountToken)) == 1 {
		return productOperation{}, errProductConfig
	}
	accountReader, err := provideridentity.NewReader(authenticateBearer(accountToken), nil)
	if err != nil {
		return productOperation{}, errProductConfig
	}
	revoker, err := providerlifecycle.NewClient(authenticateBearer(revokerToken), nil)
	if err != nil {
		return productOperation{}, errProductConfig
	}
	target := provideridentity.CloudflareAccountToken{
		Reader: reader, AccountReader: &accountReader, PlanTarget: account, AccountID: account,
		TokenID: oldID, ExpectedName: oldName, ExpectedIssuedOn: issuedOn, ExpectedPolicyDigest: policyDigest,
	}
	loadEvidence := func() (retirementEvidence, error) {
		return readRetirementEvidence(evidenceFile, ed25519.PublicKey(signer), registration)
	}
	recovery := effectgateway.CloudflareRevocationRecovery{
		Binding: binding, Plan: plan, CanonicalTarget: registration.CanonicalTarget, Token: target,
	}
	return productOperation{binding: binding, recovery: recovery, resolve: func(ctx context.Context, req effectgateway.Request) (effectgateway.RotationAction, error) {
		evidence, err := loadEvidence()
		if err != nil {
			return nil, effectgateway.ErrAdmissionRejected
		}
		retirement := CloudflareRetirement{
			MutationID: mutation, Target: target, Revoker: revoker,
			Activation: evidence.activation, Consumers: evidence.consumers,
			VerifyActivation: func(_ context.Context, plan rotation.PreparationPlan, activation rotation.ActivationEvidence) error {
				fresh, err := loadEvidence()
				if err != nil || plan.ID != binding.PlanDigest || fresh.activation != activation || !slices.Equal(fresh.consumers, evidence.consumers) {
					return effectgateway.ErrAdmissionRejected
				}
				return nil
			},
			VerifyConsumer: func(_ context.Context, plan rotation.PreparationPlan, consumer rotation.ConsumerEvidence) error {
				// VerifyActivation checked the complete signed batch for this attestation.
				if plan.ID != binding.PlanDigest || !slices.Contains(evidence.consumers, consumer) {
					return effectgateway.ErrAdmissionRejected
				}
				return nil
			},
			Authorize: func(ctx context.Context, operation string, plan rotation.PreparationPlan, _ rotation.RetirementDecision) (rotation.RevocationGrant, error) {
				if ctx.Err() != nil || operation != mutation || plan.ID != binding.PlanDigest {
					return rotation.RevocationGrant{}, effectgateway.ErrAdmissionRejected
				}
				return rotation.RevocationGrant{
					PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest, OldCredentialHandle: oldID,
					OldIdentityDigest: oldIdentity, PreapprovedOperation: mutation,
					MinimumGrace: time.Duration(graceSeconds) * time.Second, ExpiresAt: expiry,
				}, nil
			},
		}
		return (CloudflareRotation{Plan: plan, AccountID: account, Retirement: &retirement}).ResolveRotation(ctx, req)
	}}, nil
}
