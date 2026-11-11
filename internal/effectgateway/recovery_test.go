package effectgateway

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
)

type recoveryAuthorizerFunc func(context.Context, authoritybinding.Binding) (RecoveryGrant, error)

func (f recoveryAuthorizerFunc) AuthorizeRecovery(ctx context.Context, binding authoritybinding.Binding) (RecoveryGrant, error) {
	return f(ctx, binding)
}

type recoveryObserverFunc func(context.Context, authoritybinding.Binding) (RecoveryEvidence, error)

func (f recoveryObserverFunc) ObserveRecovery(ctx context.Context, binding authoritybinding.Binding) (RecoveryEvidence, error) {
	return f(ctx, binding)
}

func TestReconcileRequiresFreshReadGrantAndCorroboratedOriginal(t *testing.T) {
	for _, tc := range []struct {
		name      string
		wantCalls int
		wantState journal.State
	}{
		{"expired grant", 0, journal.UnknownEffect},
		{"write grant reused", 0, journal.UnknownEffect},
		{"wrong provider", 1, journal.UnknownEffect},
		{"stale observation", 1, journal.UnknownEffect},
		{"inconclusive observation", 1, journal.UnknownEffect},
		{"corroborated", 1, journal.ObservedApplied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			req := testRequest()
			intent := journal.Intent{OperationID: req.MutationID, TargetDigest: req.TargetDigest, PlanDigest: req.PlanDigest, GrantDigest: req.GrantDigest}
			binding := authoritybinding.Binding{
				GoalID: req.GoalID, MutationID: req.MutationID, WriterGeneration: req.Generation, Owner: req.Owner,
				Session: req.Session, Provider: req.Provider, Operation: "recoverable", ResourceDigest: strings.Repeat("e", 64),
				TargetDigest: req.TargetDigest, PlanDigest: req.PlanDigest, GrantDigest: req.GrantDigest,
			}
			_, err := h.gw.Journal.DispatchBound(intent, binding, func(journal.DispatchGuard) (journal.Observation, error) {
				return journal.Observation{}, errors.New("synthetic disconnect")
			})
			if !errors.Is(err, journal.ErrUnknownEffect) {
				t.Fatalf("setup dispatch: %v", err)
			}
			identity := strings.Repeat("f", 64)
			readGrant := RecoveryGrant{Binding: binding, GrantDigest: strings.Repeat("a", 64), ProviderIdentityDigest: identity, ExpiresAt: testNow.Add(time.Minute)}
			if readGrant.GrantDigest == binding.GrantDigest {
				readGrant.GrantDigest = strings.Repeat("9", 64)
			}
			h.gw.RecoveryAuthority = recoveryAuthorizerFunc(func(_ context.Context, actual authoritybinding.Binding) (RecoveryGrant, error) {
				if actual != binding {
					t.Fatalf("authorizer received a different binding: %+v", actual)
				}
				switch tc.name {
				case "expired grant":
					readGrant.ExpiresAt = testNow
				case "write grant reused":
					readGrant.GrantDigest = binding.GrantDigest
				}
				return readGrant, nil
			})
			calls := 0
			h.gw.RecoveryObserver = recoveryObserverFunc(func(_ context.Context, actual authoritybinding.Binding) (RecoveryEvidence, error) {
				calls++
				if actual != binding {
					t.Fatalf("observer received a different binding: %+v", actual)
				}
				evidence := RecoveryEvidence{State: journal.ObservedApplied, ObservedAt: testNow, ProviderIdentityDigest: identity, SourceDigest: strings.Repeat("d", 64)}
				switch tc.name {
				case "wrong provider":
					evidence.ProviderIdentityDigest = strings.Repeat("1", 64)
				case "stale observation":
					evidence.ObservedAt = testNow.Add(-time.Second)
				case "inconclusive observation":
					evidence.State = journal.UnknownEffect
				}
				return evidence, nil
			})
			record, err := h.gw.Reconcile(context.Background(), intent.OperationID)
			if record.State != tc.wantState || calls != tc.wantCalls || (err != nil) != (tc.wantState == journal.UnknownEffect) {
				t.Fatalf("reconcile: state=%s calls=%d err=%v", record.State, calls, err)
			}
			persisted, err := h.gw.Journal.Status(intent.OperationID)
			if err != nil || persisted.State != tc.wantState || persisted.Binding == nil || *persisted.Binding != binding {
				t.Fatalf("durable record: %+v, %v", persisted, err)
			}
		})
	}
}
