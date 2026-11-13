package providers

import (
	"context"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/cloudflaredelivery"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
)

// CloudflareIssueRecovery reconciles a lost acknowledgement by joining the
// original operation's private store record with independent provider metadata.
type CloudflareIssueRecovery struct {
	Binding   authoritybinding.Binding
	AccountID string
	Issue     CloudflareTokenIssue
}

func (r CloudflareIssueRecovery) IdentityDigest() string {
	return hostbridge.TargetDigest("cloudflare", "recover-issued-account-token", strings.Join([]string{r.AccountID, r.Issue.Name, r.Issue.PermissionGroupID, r.Issue.ExpiresAt.UTC().Truncate(time.Second).Format(time.RFC3339)}, "/"))
}

func (r CloudflareIssueRecovery) ObserveRecovery(ctx context.Context, binding authoritybinding.Binding) (effectgateway.RecoveryEvidence, error) {
	issue := r.Issue
	canonical := r.AccountID + "/tokens/" + issue.Name + "/permission-group/" + issue.PermissionGroupID
	if binding != r.Binding || binding.Provider != "cloudflare" ||
		binding.MutationID != issue.MutationID || issue.Sink == nil ||
		binding.TargetDigest != hostbridge.TargetDigest("cloudflare", cloudflareIssueOperation, canonical) {
		return effectgateway.RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	tokenID, found, err := issue.Sink.Lookup(ctx, binding.MutationID)
	if err != nil || !found || !cloudflareID(tokenID) {
		return effectgateway.RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	if err := issue.PermissionReader.VerifyIssuedWorkersToken(ctx, r.AccountID, tokenID, issue.Name, issue.PermissionGroupID, issue.ExpiresAt); err != nil {
		return effectgateway.RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	identity := r.IdentityDigest()
	return effectgateway.RecoveryEvidence{
		State: journal.ObservedApplied, ObservedAt: time.Now().UTC(), ProviderIdentityDigest: identity,
		SourceDigest: hostbridge.OperationDigest(binding.MutationID, hostbridge.OperationDigest(identity, tokenID)),
	}, nil
}

// CloudflareWorkerStageRecovery observes an independently tagged inactive
// version against the original deployment baseline using a read credential.
type CloudflareWorkerStageRecovery struct {
	Binding                                     authoritybinding.Binding
	AccountID, Worker, Environment, Destination string
	ExpectedActiveDigest                        string
	Observer                                    cloudflaredelivery.Observer
}

func (r CloudflareWorkerStageRecovery) targetDigest() string {
	return hostbridge.TargetDigest("cloudflare", cloudflareStageOperation, strings.Join([]string{r.AccountID, r.Worker, r.Environment, r.Destination}, "/"))
}

func (r CloudflareWorkerStageRecovery) IdentityDigest() string {
	return hostbridge.OperationDigest(r.targetDigest(), r.ExpectedActiveDigest)
}

func (r CloudflareWorkerStageRecovery) ObserveRecovery(ctx context.Context, binding authoritybinding.Binding) (effectgateway.RecoveryEvidence, error) {
	if binding != r.Binding || binding.Provider != "cloudflare" || binding.TargetDigest != r.targetDigest() {
		return effectgateway.RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	version, err := cloudflaredelivery.FindStagedVersion(ctx, r.Observer, r.AccountID, r.Worker, r.Destination, "kc-"+binding.MutationID, r.ExpectedActiveDigest)
	if err != nil {
		return effectgateway.RecoveryEvidence{}, journal.ErrUnknownEffect
	}
	identity := r.IdentityDigest()
	return effectgateway.RecoveryEvidence{
		State: journal.ObservedApplied, ObservedAt: time.Now().UTC(), ProviderIdentityDigest: identity,
		SourceDigest: hostbridge.OperationDigest(binding.MutationID, hostbridge.OperationDigest(identity, version)),
	}, nil
}
