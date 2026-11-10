// Package effectgateway implements the single effect boundary decided in
// workspace-infra docs/contracts/effect-fencing-decision-v1.md.
//
// Writers submit signed intents. The gateway is the only component that may call
// a provider adapter, and it re-checks the claim immediately before that call.
// Claim state itself is owned by the existing ClaimStore; this package consumes a
// verified snapshot and never re-implements claim storage.
//
// Guarantees within one process:
//   - one durable journal intent per mutation id (duplicates never reach a provider),
//   - stale generation, wrong session and wrong owner are refused before any effect,
//   - an unknown provider result stays UNKNOWN_EFFECT and is never retried here,
//   - the journal writer lock serialises effect execution for one journal root.
//
// Not guaranteed: OS isolation from a same-UID process, and exactly-once semantics
// at the provider (Cloudflare/GitHub/OCI expose no fencing token or idempotency key).
package effectgateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

// Request status values reported to the writer.
const (
	StatusApplied   = "APPLIED"
	StatusUnknown   = "UNKNOWN_EFFECT"
	StatusDuplicate = "DUPLICATE"
	StatusNoEffect  = "NO_EFFECT"
)

var (
	ErrNotConfigured     = errors.New("effect gateway is not fully configured")
	ErrInvalidRequest    = errors.New("effect gateway request is invalid")
	ErrUnauthenticated   = errors.New("effect gateway request is not authenticated")
	ErrExpired           = errors.New("effect gateway request is expired")
	ErrAuthorityUnavail  = errors.New("effect gateway cannot verify the claim")
	ErrStaleGeneration   = errors.New("effect gateway refused a stale writer generation")
	ErrSessionMismatch   = errors.New("effect gateway refused a different writer session")
	ErrOwnerMismatch     = errors.New("effect gateway refused a different writer owner")
	ErrClaimNotActive    = errors.New("effect gateway requires an ACTIVE claim")
	ErrClaimLeaseExpired = errors.New("effect gateway requires a live claim lease")
	ErrAdmissionRejected = errors.New("host admission rejected the effect")
	ErrTargetPending     = errors.New("effect gateway has an unresolved operation for this target")
	ErrGatewayBusy       = errors.New("effect gateway is busy for this journal")
)

// Request is the writer's intent. ExpiresAt bounds replay; MutationID is the
// durable idempotency key.
type Request struct {
	GoalID       string
	MutationID   string
	Provider     string
	Action       string
	TargetDigest string
	PlanDigest   string
	GrantDigest  string
	Generation   int64
	Session      string
	Owner        string
	ExpiresAt    time.Time
}

// Envelope is the wire form: request plus its HMAC-SHA256 signature.
type Envelope struct {
	Request   Request
	Signature string
}

// ClaimSnapshot is a verified view supplied by the existing ClaimStore adapter.
type ClaimSnapshot struct {
	GoalID         string
	Generation     int64
	Session        string
	Owner          string
	State          string
	LeaseExpiresAt time.Time
}

// ClaimAuthority reads the current claim. It must fail closed on any error.
type ClaimAuthority interface {
	Snapshot(ctx context.Context, goalID string) (ClaimSnapshot, error)
}

// KeyRing returns the per-session signing key provisioned by the host at claim
// time. A missing key is an authentication failure, not an authorisation bypass.
type KeyRing interface {
	Key(ctx context.Context, goalID, owner, session string, generation int64) ([]byte, error)
}

// Authorizer is satisfied by *hostbridge.Bridge.
type Authorizer interface {
	Authorize(ctx context.Context, provider, operation, target string) error
}

// EffectResult is what the provider adapter reports. A gateway-local decision that
// no provider call happened uses StateObservedNoEffect.
type EffectResult struct {
	State       string
	ProofDigest string
}

const (
	StateAcknowledged     = "ACKNOWLEDGED"
	StateObservedNoEffect = "OBSERVED_NO_EFFECT"
)

// Provider is the only provider entry point the gateway may call.
type Provider interface {
	Apply(ctx context.Context, req Request) (EffectResult, error)
}

// Receipt is the writer-visible outcome. It never claims provider truth.
type Receipt struct {
	MutationID     string
	Status         string
	ProviderCalls  int
	OperationState string
	ProofDigest    string
	Note           string
}

const (
	maxRequestTTL = 10 * time.Minute
	minKeyBytes   = 32
)

func (r Request) validate(now time.Time) error {
	for name, value := range map[string]string{
		"goal_id": r.GoalID, "mutation_id": r.MutationID, "provider": r.Provider,
		"action": r.Action, "session": r.Session, "owner": r.Owner,
	} {
		if strings.TrimSpace(value) == "" || len(value) > 200 {
			return fmt.Errorf("%w: %s is empty or too long", ErrInvalidRequest, name)
		}
	}
	if !journal.ValidOperationID(r.MutationID) {
		return fmt.Errorf("%w: mutation_id is not canonical", ErrInvalidRequest)
	}
	for name, value := range map[string]string{
		"target_digest": r.TargetDigest, "plan_digest": r.PlanDigest, "grant_digest": r.GrantDigest,
	} {
		if !journal.ValidDigest(value) {
			return fmt.Errorf("%w: %s must be a sha256 hex digest", ErrInvalidRequest, name)
		}
	}
	if r.Generation < 1 {
		return fmt.Errorf("%w: generation must be positive", ErrInvalidRequest)
	}
	if r.ExpiresAt.IsZero() {
		return fmt.Errorf("%w: expires_at is required", ErrInvalidRequest)
	}
	if !now.Before(r.ExpiresAt) {
		return ErrExpired
	}
	if r.ExpiresAt.After(now.Add(maxRequestTTL)) {
		return fmt.Errorf("%w: expiry is too far in the future", ErrInvalidRequest)
	}
	return nil
}

// signingPayload is unambiguous: every field is length-prefixed.
func (r Request) signingPayload() []byte {
	fields := []string{
		r.GoalID, r.MutationID, r.Provider, r.Action, r.TargetDigest, r.PlanDigest,
		r.GrantDigest, strconv.FormatInt(r.Generation, 10), r.Session, r.Owner,
		r.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	var b strings.Builder
	for _, f := range fields {
		fmt.Fprintf(&b, "%d:%s|", len(f), f)
	}
	return []byte(b.String())
}

// Sign produces the envelope signature with a session key.
func Sign(req Request, key []byte) Envelope {
	mac := hmac.New(sha256.New, key)
	mac.Write(req.signingPayload())
	return Envelope{Request: req, Signature: hex.EncodeToString(mac.Sum(nil))}
}

func verifySignature(env Envelope, key []byte) bool {
	if len(key) < minKeyBytes || env.Signature == "" {
		return false
	}
	want, err := hex.DecodeString(env.Signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(env.Request.signingPayload())
	return hmac.Equal(mac.Sum(nil), want)
}
