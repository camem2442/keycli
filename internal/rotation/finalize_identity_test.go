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

func TestFinalizeRejectsProviderMismatchesBeforeEffect(t *testing.T) {
	p := planFixture(t)
	now := time.Now()
	activation := activationFixture(now)
	consumers := evidenceFixture(now)
	data, _ := json.Marshal(struct {
		Activation ActivationEvidence
		Consumers  []ConsumerEvidence
	}{activation, consumers})
	digest := sha256.Sum256(data)
	for _, test := range []struct {
		name             string
		mutate           func(*VerifiedOldCredential)
		grantDigestWrong bool
		noObserver       bool
	}{
		{"wrong_old_credential_id", func(x *VerifiedOldCredential) { x.CredentialID = strings.Repeat("2", 32) }, false, false},
		{"wrong_tenant", func(x *VerifiedOldCredential) { x.AccountID = strings.Repeat("b", 32) }, false, false},
		{"wrong_target", func(x *VerifiedOldCredential) { x.PlanTarget = "other" }, false, false},
		{"wrong_generation", func(x *VerifiedOldCredential) { x.Generation = 2 }, false, false},
		{"grant_observation_mismatch", func(*VerifiedOldCredential) {}, true, false},
		{"provider_read_failed", func(*VerifiedOldCredential) {}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			revoked := 0
			_, err := Finalize(context.Background(), rotationStore(t), "revoke-one", p, activation, consumers, now, verifyActivationOK, verifyConsumerOK,
				func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error) {
					d := RevocationIdentityDigest(fixtureOldCredential(p, cloudflareOldHandle()))
					if test.grantDigestWrong {
						d = strings.Repeat("f", 64)
					}
					return RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("e", 64), OldCredentialHandle: cloudflareOldHandle(), OldIdentityDigest: d, EvidenceDigest: hex.EncodeToString(digest[:]), ExpiresAt: time.Now().Add(time.Minute)}, nil
				},
				func(_ context.Context, plan PreparationPlan, handle string) (VerifiedOldCredential, error) {
					if test.noObserver {
						return VerifiedOldCredential{}, errors.New("provider unavailable")
					}
					id := fixtureOldCredential(plan, handle)
					test.mutate(&id)
					return id, nil
				},
				func(context.Context, Generation, string) (journal.Observation, error) {
					revoked++
					return journal.Observation{State: journal.Acknowledged, ProofDigest: strings.Repeat("f", 64)}, nil
				})
			if err == nil || revoked != 0 {
				t.Fatalf("unsafe revoke %v, calls=%d", err, revoked)
			}
		})
	}
}

// The provider-specific revokers below are synthetic callbacks, not real APIs.
// They prove that Finalize passes exactly the independently observed old ID
// and only after a separately bound grant and consumer re-attestation.
func TestFinalizeExactProviderRevokers(t *testing.T) {
	for _, tc := range []struct {
		name, provider, oldID string
	}{
		{"github_generation_scoped_secret", "github", "FOO_GEN_1"},
		{"cloudflare_account_api_token", "cloudflare", cloudflareOldHandle()},
		{"oracle_vault_secret_version", "oracle", "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := Generation{Alias: "one-credential", Kind: StaticServiceKey, Provider: tc.provider, Target: "target-fixture", Number: 1}
			next := old
			next.Number = 2
			p, err := NewPreparationPlan(old, next, []Consumer{{ID: "agent", Required: true}})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			act := ActivationEvidence{StageReceiptDigest: strings.Repeat("a", 64), PromotionReceiptDigest: strings.Repeat("b", 64), ActivatedAt: now.Add(-time.Hour), GraceUntil: now.Add(-time.Minute)}
			cons := []ConsumerEvidence{{ConsumerID: "agent", Generation: 2, ObservedAt: now.Add(-time.Minute), EvidenceDigest: strings.Repeat("c", 64)}}
			payload, _ := json.Marshal(struct {
				Activation ActivationEvidence
				Consumers  []ConsumerEvidence
			}{act, cons})
			sum := sha256.Sum256(payload)
			calls := 0
			out, err := Finalize(context.Background(), rotationStore(t), "old-revoke", p, act, cons, now,
				func(context.Context, PreparationPlan, ActivationEvidence) error { return nil },
				func(context.Context, PreparationPlan, ConsumerEvidence) error { return nil },
				func(context.Context, string, PreparationPlan, RetirementDecision) (RevocationGrant, error) {
					return RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("d", 64), OldCredentialHandle: tc.oldID,
						OldIdentityDigest: RevocationIdentityDigest(fixtureOldCredential(p, tc.oldID)),
						EvidenceDigest:    hex.EncodeToString(sum[:]), ExpiresAt: time.Now().Add(time.Minute)}, nil
				},
				fixtureIdentityVerifier,
				func(_ context.Context, got Generation, handle string) (journal.Observation, error) {
					calls++
					if got != p.Old || handle != tc.oldID {
						t.Fatal("wrong provider old identity")
					}
					return journal.Observation{State: journal.Acknowledged, ProofDigest: strings.Repeat("f", 64)}, nil
				})
			if err != nil || out.State != journal.Acknowledged || calls != 1 {
				t.Fatalf("%s : %+v, %v, %d", tc.name, out, err, calls)
			}
		})
	}
}
