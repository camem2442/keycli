package effectgateway

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
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
)

type rotationResolverFunc func(context.Context, Request) (RotationAction, error)

func (f rotationResolverFunc) ResolveRotation(ctx context.Context, req Request) (RotationAction, error) {
	return f(ctx, req)
}

func TestGatewayOwnsRotationDispatchAndFence(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "authorized"
		if denied {
			name = "denied at execution"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t)
			req := testRequest()
			old := rotation.Generation{Alias: "fixture", Kind: rotation.StaticServiceKey, Provider: req.Provider, Target: "account", Number: 1}
			next := old
			next.Number++
			plan, err := rotation.NewPreparationPlan(old, next, []rotation.Consumer{{ID: "runner", Required: true}})
			if err != nil {
				t.Fatal(err)
			}
			req.PlanDigest = plan.ID
			fences, mutations := 0, 0
			h.gw.Authority = boundaryAuthority{before: func() {}, verify: func() error {
				fences++
				if denied {
					return hostbridge.ErrAdmissionRejected
				}
				return nil
			}}
			action, err := rotation.NewPrepareAction(ctx, req.MutationID, plan,
				func(context.Context, string, rotation.PreparationPlan) (rotation.PreparationGrant, error) {
					return rotation.PreparationGrant{PlanDigest: plan.ID, GrantDigest: req.GrantDigest, ExpiresAt: time.Now().Add(time.Minute)}, nil
				}, func(context.Context, rotation.PreparationPlan) (journal.Observation, error) {
					mutations++
					if _, err := h.gw.Journal.Dispatch(actionIntent(req), func() (journal.Observation, error) {
						t.Fatal("another dispatch entered while the mutation was running")
						return journal.Observation{}, nil
					}); !errors.Is(err, journal.ErrBusy) {
						t.Fatalf("Gateway did not hold the journal lock: %v", err)
					}
					if fences != 1 {
						t.Fatalf("mutation saw %d execution checks", fences)
					}
					return journal.Observation{State: journal.Acknowledged, ProofDigest: testPlan}, nil
				})
			if err != nil {
				t.Fatal(err)
			}
			h.gw.RotationActions = rotationResolverFunc(func(context.Context, Request) (RotationAction, error) {
				return action, nil
			})
			envelope := Sign(req, []byte(testKey))
			receipt, err := h.gw.Submit(ctx, envelope)
			wantStatus, wantCalls := StatusApplied, 1
			if denied {
				wantStatus, wantCalls = StatusNoEffect, 0
			}
			if err != nil || receipt.Status != wantStatus || receipt.ProviderCalls != wantCalls || mutations != wantCalls || fences != 1 {
				t.Fatalf("receipt=%+v fences=%d mutations=%d error=%v", receipt, fences, mutations, err)
			}
			replay, err := h.gw.Submit(ctx, envelope)
			if err != nil || replay.Status != StatusDuplicate || fences != 1 || mutations != wantCalls || h.provider.calls != 0 {
				t.Fatalf("replay=%+v fences=%d mutations=%d generic calls=%d error=%v", replay, fences, mutations, h.provider.calls, err)
			}
		})
	}
}

func actionIntent(req Request) journal.Intent {
	return journal.Intent{OperationID: req.MutationID, TargetDigest: req.TargetDigest, PlanDigest: req.PlanDigest, GrantDigest: req.GrantDigest}
}

func TestGatewayFinalizeChecksIdentityAndAuthorityBeforeRevocation(t *testing.T) {
	for _, tc := range []struct {
		name          string
		drift, denied bool
		want          []string
	}{
		{name: "approved", want: []string{"identity", "fence", "revoke"}},
		{name: "different identity", drift: true, want: []string{"identity"}},
		{name: "denied at execution", denied: true, want: []string{"identity", "fence"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newHarness(t)
			req := testRequest()
			req.Action = hostbridge.ActionRevoke
			old := rotation.Generation{Alias: "fixture", Kind: rotation.StaticServiceKey, Provider: req.Provider, Target: "account", Number: 1}
			next := old
			next.Number++
			plan, err := rotation.NewPreparationPlan(old, next, []rotation.Consumer{{ID: "runner", Required: true}})
			if err != nil {
				t.Fatal(err)
			}
			req.PlanDigest = plan.ID
			now := time.Now()
			activation := rotation.ActivationEvidence{StageReceiptDigest: strings.Repeat("a", 64), PromotionReceiptDigest: strings.Repeat("b", 64), ActivatedAt: now.Add(-time.Hour), GraceUntil: now.Add(-time.Minute)}
			consumers := []rotation.ConsumerEvidence{{ConsumerID: "runner", Generation: next.Number, ObservedAt: now.Add(-time.Second), EvidenceDigest: strings.Repeat("c", 64)}}
			raw, err := json.Marshal(struct {
				Activation rotation.ActivationEvidence
				Consumers  []rotation.ConsumerEvidence
			}{activation, consumers})
			if err != nil {
				t.Fatal(err)
			}
			evidence := sha256.Sum256(raw)
			account, handle := strings.Repeat("a", 32), strings.Repeat("b", 32)
			identity := rotation.VerifiedOldCredential{
				Provider: req.Provider, Kind: "cloudflare-account-token", PlanDigest: plan.ID, PlanTarget: old.Target,
				AccountID: account, ResourceID: account, CredentialID: handle, Generation: old.Number,
				ObservedAt: now, ProofDigest: strings.Repeat("d", 64),
			}
			var calls []string
			h.gw.Authority = boundaryAuthority{before: func() {}, verify: func() error {
				calls = append(calls, "fence")
				if tc.denied {
					return hostbridge.ErrAdmissionRejected
				}
				return nil
			}}
			action, err := rotation.NewFinalizeAction(ctx, req.MutationID, plan, activation, consumers, now,
				func(context.Context, rotation.PreparationPlan, rotation.ActivationEvidence) error { return nil },
				func(context.Context, rotation.PreparationPlan, rotation.ConsumerEvidence) error { return nil },
				func(context.Context, string, rotation.PreparationPlan, rotation.RetirementDecision) (rotation.RevocationGrant, error) {
					return rotation.RevocationGrant{
						PlanDigest: plan.ID, GrantDigest: req.GrantDigest, OldCredentialHandle: handle,
						OldIdentityDigest: rotation.RevocationIdentityDigest(identity), EvidenceDigest: hex.EncodeToString(evidence[:]),
						ExpiresAt: now.Add(time.Minute),
					}, nil
				}, func(context.Context, rotation.PreparationPlan, string) (rotation.VerifiedOldCredential, error) {
					calls = append(calls, "identity")
					observed := identity
					if tc.drift {
						observed.CredentialID = strings.Repeat("f", 32)
					}
					return observed, nil
				}, func(_ context.Context, gen rotation.Generation, credential string) (journal.Observation, error) {
					calls = append(calls, "revoke")
					if gen != old || credential != handle {
						t.Fatal("revocation targeted a different generation")
					}
					if _, err := h.gw.Journal.Dispatch(actionIntent(req), func() (journal.Observation, error) {
						t.Fatal("another dispatch entered while revoking")
						return journal.Observation{}, nil
					}); !errors.Is(err, journal.ErrBusy) {
						t.Fatalf("Gateway did not hold the journal lock: %v", err)
					}
					return journal.Observation{State: journal.Acknowledged, ProofDigest: testPlan}, nil
				})
			if err != nil {
				t.Fatal(err)
			}
			h.gw.RotationActions = rotationResolverFunc(func(context.Context, Request) (RotationAction, error) { return action, nil })
			envelope := Sign(req, []byte(testKey))
			receipt, err := h.gw.Submit(ctx, envelope)
			wantStatus, wantProviderCalls := StatusApplied, 1
			if tc.drift || tc.denied {
				wantStatus, wantProviderCalls = StatusNoEffect, 0
			}
			if err != nil || receipt.Status != wantStatus || receipt.ProviderCalls != wantProviderCalls || !slices.Equal(calls, tc.want) {
				t.Fatalf("receipt=%+v calls=%v error=%v", receipt, calls, err)
			}
			replay, err := h.gw.Submit(ctx, envelope)
			if err != nil || replay.Status != StatusDuplicate || !slices.Equal(calls, tc.want) || h.provider.calls != 0 {
				t.Fatalf("replay=%+v calls=%v generic calls=%d error=%v", replay, calls, h.provider.calls, err)
			}
		})
	}
}
