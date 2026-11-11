package effectgateway

import (
	"context"
	"errors"
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
