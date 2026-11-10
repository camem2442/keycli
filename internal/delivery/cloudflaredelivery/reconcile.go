package cloudflaredelivery

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/camem2442/keycli/internal/delivery"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/target/cloudflare"
)

// Reconcile performs bounded, read-only provider observation for a journaled
// UNKNOWN_EFFECT. It never retries Wrangler or reuses a previously issued
// mutation grant. The callback must re-authorize the *read* operation.
func Reconcile(ctx context.Context, store journal.Store, id string, plan delivery.DeliveryPlan, observeAuthorized Authorize, remote *cloudflare.VersionRESTObserver) (journal.Record, error) {
	if remote == nil {
		return journal.Record{}, errors.New("Cloudflare provider observer required")
	}
	return reconcile(ctx, store, id, plan, observeAuthorized, remote)
}

func reconcile(ctx context.Context, store journal.Store, id string, plan delivery.DeliveryPlan, authorize Authorize, remote observer) (journal.Record, error) {
	if authorize == nil || remote == nil || plan.Provider != "cloudflare-workers" || plan.Effect != "inactive-version" || plan.Source == nil || len(id) == 0 || len(id) > 96 || !validOpID(id) || plan.Authoritative || plan.AcceptanceClaim || plan.AuthorizesExecution {
		return journal.Record{}, errors.New("invalid Cloudflare reconciliation plan")
	}
	source := *plan.Source
	plan.Source = &delivery.SecretRef{Alias: source.Alias, CanonicalName: source.CanonicalName}
	plan.Unverified = slices.Clone(plan.Unverified)
	planID := plan.ID
	plan.ID = ""
	planBytes, err := jsonEncode(plan)
	if err != nil || digest(planBytes) != planID {
		return journal.Record{}, errors.New("Cloudflare reconciliation plan digest mismatch")
	}
	plan.ID = planID
	grant, err := authorize(ctx, id, plan)
	if err != nil || !time.Now().Before(grant.ExpiresAt) || grant.PlanDigest != planID ||
		!sha256Digest(grant.GrantDigest) || !sha256Digest(grant.ExpectedActiveDigest) ||
		grant.Worker != plan.Target.Worker || grant.Environment != plan.Target.Environment || grant.Destination != plan.Target.Destination ||
		!cloudflareAccount(grant.AccountID) {
		return journal.Record{}, errors.New("Cloudflare read-only reconciliation not admitted")
	}
	ctx, cancel := context.WithDeadline(ctx, grant.ExpiresAt)
	defer cancel()
	targetDigest := digestTarget(grant)
	intent := journal.Intent{OperationID: id, TargetDigest: targetDigest, PlanDigest: planID, GrantDigest: grant.GrantDigest}
	return store.Reconcile(intent, func(record journal.Record) (journal.Observation, error) {
		if record.State == journal.IntentWritten {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		snapshot, err := remote.Observe(ctx, grant.AccountID, grant.Worker)
		if err != nil || snapshot.AccountID != grant.AccountID || snapshot.Worker != grant.Worker || activeDigest(snapshot) != grant.ExpectedActiveDigest {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		// Even if the list is truncated, the operation is held UNKNOWN rather
		// than concluding absence. Limit GETs for reconciliation costs.
		if len(snapshot.LatestVersionIDs) > 8 || len(snapshot.LatestVersionIDs) == 0 {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		active := map[string]bool{}
		for _, part := range snapshot.Versions {
			active[part.VersionID] = true
		}
		var candidate string
		for _, versionID := range snapshot.LatestVersionIDs {
			detail, err := remote.GetVersion(ctx, grant.AccountID, grant.Worker, versionID)
			if err != nil {
				return journal.Observation{}, journal.ErrUnknownEffect
			}
			if detail.Tag == "kc-"+id && detail.ID == versionID && slices.Contains(detail.BindingNames, grant.Destination) && !active[versionID] {
				if candidate != "" {
					return journal.Observation{}, journal.ErrUnknownEffect
				}
				candidate = versionID
			}
		}
		if candidate == "" {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		proof := digest([]byte(record.OperationID + "\x00" + record.TargetDigest + "\x00" + candidate + "\x00" + activeDigest(snapshot)))
		return journal.Observation{State: journal.ObservedApplied, ProofDigest: proof}, nil
	})
}
