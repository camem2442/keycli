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

func reconcile(ctx context.Context, store journal.Store, id string, plan delivery.DeliveryPlan, authorize Authorize, remote Observer) (journal.Record, error) {
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
		!sha256Digest(grant.GrantDigest) || !sha256Digest(grant.OriginalWriteGrantDigest) ||
		grant.GrantDigest == grant.OriginalWriteGrantDigest || !sha256Digest(grant.ExpectedActiveDigest) ||
		grant.Worker != plan.Target.Worker || grant.Environment != plan.Target.Environment || grant.Destination != plan.Target.Destination ||
		!cloudflareAccount(grant.AccountID) {
		return journal.Record{}, errors.New("Cloudflare read-only reconciliation not admitted")
	}
	ctx, cancel := context.WithDeadline(ctx, grant.ExpiresAt)
	defer cancel()
	targetDigest := digestTarget(grant)
	// The read authorization is fresh and distinct from the original write
	// grant. Reconcile only the exact journal record of that older write.
	intent := journal.Intent{OperationID: id, TargetDigest: targetDigest, PlanDigest: planID, GrantDigest: grant.OriginalWriteGrantDigest}
	return store.Reconcile(intent, func(record journal.Record) (journal.Observation, error) {
		if record.State == journal.IntentWritten {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		candidate, err := FindStagedVersion(ctx, remote, grant.AccountID, grant.Worker, grant.Destination, "kc-"+id, grant.ExpectedActiveDigest)
		if err != nil {
			return journal.Observation{}, err
		}
		proof := digest([]byte(record.OperationID + "\x00" + record.TargetDigest + "\x00" + candidate + "\x00" + grant.ExpectedActiveDigest))
		return journal.Observation{State: journal.ObservedApplied, ProofDigest: proof}, nil
	})
}

// FindStagedVersion finds an exact tagged inactive version under the original
// deployment baseline. It never infers absence from the bounded inventory.
func FindStagedVersion(ctx context.Context, remote Observer, account, worker, destination, tag, expectedActiveDigest string) (string, error) {
	if remote == nil || tag == "" || !sha256Digest(expectedActiveDigest) {
		return "", journal.ErrUnknownEffect
	}
	snapshot, err := remote.Observe(ctx, account, worker)
	if err != nil || snapshot.AccountID != account || snapshot.Worker != worker || activeDigest(snapshot) != expectedActiveDigest {
		return "", journal.ErrUnknownEffect
	}
	// A full page cannot establish uniqueness of the operation tag.
	if len(snapshot.LatestVersionIDs) >= cloudflare.VersionInventoryLimit || len(snapshot.LatestVersionIDs) == 0 {
		return "", journal.ErrUnknownEffect
	}
	active := map[string]bool{}
	for _, part := range snapshot.Versions {
		active[part.VersionID] = true
	}
	var candidate string
	for _, versionID := range snapshot.LatestVersionIDs {
		detail, err := remote.GetVersion(ctx, account, worker, versionID)
		if err != nil || detail.ID != versionID {
			return "", journal.ErrUnknownEffect
		}
		if detail.Tag == tag {
			if candidate != "" || !slices.Contains(detail.BindingNames, destination) || active[versionID] {
				return "", journal.ErrUnknownEffect
			}
			candidate = versionID
		}
	}
	if candidate == "" {
		return "", journal.ErrUnknownEffect
	}
	return candidate, nil
}
