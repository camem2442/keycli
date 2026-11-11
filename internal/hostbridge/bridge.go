package hostbridge

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
)

// MaxAdmissionLifetime bounds how long a host decision may remain usable. A
// decision that outlives a single effect attempt is not a decision any more.
const MaxAdmissionLifetime = 10 * time.Minute

var (
	sha40    = regexp.MustCompile(`^[0-9a-f]{40}$`)
	repoSlug = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)
	hostID   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

// Bridge turns host-owned identity plus a trusted Admitter into a
// providerlifecycle.HostAuthorize callback.
type Bridge struct {
	Admitter Admitter
	Identity Identity
	// Now is optional; tests and hosts may pin the clock.
	Now func() time.Time
}

func (b *Bridge) now() time.Time {
	if b != nil && b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// HostAuthorize returns the callback expected by
// providerlifecycle.Client methods. Passing a nil Bridge yields a callback that
// always refuses.
func (b *Bridge) HostAuthorize() providerlifecycle.HostAuthorize {
	return b.Authorize
}

// Authorize admits or refuses exactly one provider effect.
func (b *Bridge) Authorize(ctx context.Context, provider, operation, target string) error {
	if ctx == nil || ctx.Err() != nil {
		return fmt.Errorf("%w: context is not usable", ErrAdmissionInvalid)
	}
	if b == nil || b.Admitter == nil {
		return ErrNoAdmitter
	}
	if provider == "" || operation == "" || target == "" {
		return fmt.Errorf("%w: provider, operation and target are required", ErrUnknownOperation)
	}
	action, mutation, err := ClassifyOperation(provider, operation)
	if err != nil {
		return err
	}
	request, err := b.request(provider, action, operation, target)
	if err != nil {
		return err
	}
	decision, err := b.Admitter.Admit(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAdmissionRejected, err)
	}
	if err := b.checkDecision(request, decision, mutation); err != nil {
		return err
	}
	return nil
}

func (b *Bridge) request(provider, action, operation, target string) (Request, error) {
	id := b.Identity
	if id.GoalID == "" || id.OperationID == "" || !repoSlug.MatchString(id.Repository) ||
		!sha40.MatchString(id.HeadSHA) || !sha40.MatchString(id.BaseSHA) ||
		id.HeadSHA == id.BaseSHA || id.WriterGeneration < 1 || !hostID.MatchString(id.HostIdentity) ||
		id.CredentialIsolation == "" || id.IsolationDigest == "" {
		return Request{}, fmt.Errorf("%w: host identity is incomplete", ErrAdmissionInvalid)
	}
	targetDigest := TargetDigest(provider, operation, target)
	return Request{
		GoalID:                    id.GoalID,
		OperationID:               id.OperationID,
		Repository:                id.Repository,
		HeadSHA:                   id.HeadSHA,
		BaseSHA:                   id.BaseSHA,
		Provider:                  provider,
		Action:                    action,
		TargetDigest:              targetDigest,
		OperationDigest:           OperationDigest(id.OperationID, targetDigest),
		WriterGeneration:          id.WriterGeneration,
		HostIdentity:              id.HostIdentity,
		Paths:                     append([]string(nil), id.Paths...),
		BudgetCost:                id.BudgetCost,
		CredentialIsolation:       id.CredentialIsolation,
		CredentialIsolationDigest: id.IsolationDigest,
		ReconcileOf:               id.ReconcileOf,
	}, nil
}

func (b *Bridge) checkDecision(request Request, decision Decision, mutation bool) error {
	if decision.Status != StatusAdmitted {
		reason := decision.Reason
		if reason == "" {
			reason = "no reason given"
		}
		return fmt.Errorf("%w: %s", ErrAdmissionRejected, reason)
	}
	if !decision.AuthorizesExecution {
		return fmt.Errorf("%w: decision does not authorise execution", ErrAdmissionInvalid)
	}
	if decision.GrantID == "" {
		return fmt.Errorf("%w: decision has no grant binding", ErrAdmissionInvalid)
	}
	if decision.ProviderMutation != mutation {
		return fmt.Errorf("%w: mutation class does not match the decision", ErrAdmissionInvalid)
	}
	now := b.now()
	if !decision.ExpiresAt.After(now) {
		return fmt.Errorf("%w: decision is expired", ErrAdmissionInvalid)
	}
	if decision.ExpiresAt.After(now.Add(MaxAdmissionLifetime)) {
		return fmt.Errorf("%w: decision lifetime is too long", ErrAdmissionInvalid)
	}
	if decision.Bound.Provider != request.Provider || decision.Bound.Action != request.Action ||
		decision.Bound.TargetDigest != request.TargetDigest || decision.Bound.OperationID != request.OperationID {
		return fmt.Errorf("%w: provider, action, target or operation differs", ErrBindingMismatch)
	}
	if decision.Bound.Digest() != request.Digest() {
		return fmt.Errorf("%w: request fields differ", ErrBindingMismatch)
	}
	return nil
}

// ErrNilBridge is returned when Authorize is called on a nil bridge.
var ErrNilBridge = errors.New("host bridge is nil")

// AuthorizeFunc adapts a bridge into a plain function for callers that only need
// the three provider arguments.
func AuthorizeFunc(b *Bridge) func(context.Context, string, string, string) error {
	if b == nil {
		return func(context.Context, string, string, string) error { return ErrNilBridge }
	}
	return b.Authorize
}
