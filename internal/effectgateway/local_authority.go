package effectgateway

import (
	"context"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/hostbridge"
)

// RegisteredOperation is a trusted projection of a protected registration.
// The registration resolves canonical target and independently verified digests.
// The host identity binds the registered operation and exact mutation separately.
type RegisteredOperation struct {
	Identity          hostbridge.Identity
	Provider          string
	ProviderOperation string
	CanonicalTarget   string
	ResourceDigest    string
	PlanDigest        string
	GrantDigest       string
}

type OperationRegistry interface {
	Resolve(ctx context.Context, goalID, mutationID string) (RegisteredOperation, error)
}

// LocalAuthority binds each submission independently. It does not accept a
// socket path, raw provider target, or registry contents from the writer.
type LocalAuthority struct {
	Registry OperationRegistry
	Admitter *hostbridge.LocalAdmitter
	Now      func() time.Time
}

func (a LocalAuthority) Bind(ctx context.Context, req Request) (BoundAuthority, error) {
	if a.Registry == nil || a.Admitter == nil {
		return nil, hostbridge.ErrLocalAuthorityUnavailable
	}
	reg, err := a.Registry.Resolve(ctx, req.GoalID, req.MutationID)
	if err != nil {
		return nil, ErrAdmissionRejected
	}
	id := reg.Identity
	action, mutating, err := hostbridge.ClassifyOperation(reg.Provider, reg.ProviderOperation)
	if err != nil || !mutating || req.GoalID != id.GoalID || req.MutationID != id.MutationID ||
		req.Generation != id.WriterGeneration || req.Owner != id.WriterOwner ||
		req.Session != id.WriterSession || req.Provider != reg.Provider || req.Action != action ||
		reg.CanonicalTarget == "" || strings.ContainsRune(reg.CanonicalTarget, 0) {
		return nil, hostbridge.ErrBindingMismatch
	}
	binding := reg.authorityBinding()
	if !binding.Valid() || binding.TargetDigest != req.TargetDigest || reg.PlanDigest != req.PlanDigest || reg.GrantDigest != req.GrantDigest {
		return nil, hostbridge.ErrBindingMismatch
	}
	now := a.Now
	if now == nil {
		now = time.Now
	}
	return &localBoundAuthority{
		admitter: a.Admitter,
		now:      now,
		binding:  binding,
	}, nil
}

func (reg RegisteredOperation) authorityBinding() hostbridge.AuthorityBinding {
	id := reg.Identity
	return hostbridge.AuthorityBinding{
		GoalID: id.GoalID, MutationID: id.MutationID, WriterGeneration: id.WriterGeneration,
		Owner: id.WriterOwner, Session: id.WriterSession, Provider: reg.Provider,
		Operation: id.OperationID, ResourceDigest: reg.ResourceDigest,
		TargetDigest: hostbridge.TargetDigest(reg.Provider, reg.ProviderOperation, reg.CanonicalTarget),
		PlanDigest:   reg.PlanDigest, GrantDigest: reg.GrantDigest,
	}
}

type localBoundAuthority struct {
	admitter *hostbridge.LocalAdmitter
	now      func() time.Time
	binding  hostbridge.AuthorityBinding
	expiry   time.Time
}

func (a *localBoundAuthority) Preflight(ctx context.Context) error {
	expiry, err := a.admitter.Preflight(ctx, a.binding, a.now)
	if err == nil {
		a.expiry = expiry
	}
	return err
}

func (a *localBoundAuthority) VerifyExecution(ctx context.Context) error {
	if !a.now().Before(a.expiry) {
		return hostbridge.ErrAdmissionInvalid
	}
	return a.admitter.VerifyExecution(ctx, a.binding)
}
