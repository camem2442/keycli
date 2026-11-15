// Package hostbridge provides a trust-preserving adapter for the complete
// effectgateway intent. Host observation is mandatory; this adapter creates
// no Grant, Claim, Writer, database, permission or provider resource.
package hostbridge

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"
)

// GatewayIntent is the signed intent projected into the host admission boundary.
type GatewayIntent struct {
	GoalID       string    `json:"goal_id"`
	MutationID   string    `json:"mutation_id"`
	Provider     string    `json:"provider"`
	Operation    string    `json:"operation"`
	Action       string    `json:"action"`
	TargetDigest string    `json:"target_digest"`
	PlanDigest   string    `json:"plan_digest"`
	GrantDigest  string    `json:"grant_digest"`
	Generation   int64     `json:"generation"`
	Session      string    `json:"session"`
	Owner        string    `json:"owner"`
	ExpiresAt    time.Time `json:"expires_at"`
}

var sha256Hex = regexp.MustCompile("^[a-f0-9]{64}$")

// GatewayBinding is created only by the trusted host from the actual source,
// original Grant, observed provider target and current mutation lease.
// CheckCurrent MUST independently re-read the current Goal/Writer/mutation
// lease, original Grant and source HEAD on every call, including immediately
// before a provider effect. A model supplied boolean or saved JSON is invalid.
type GatewayBinding struct {
	Bridge       *Bridge
	RawTarget    string
	ExpectedHead string
	PlanDigest   string
	GrantDigest  string
	Owner        string
	Session      string
	CheckCurrent func(context.Context, GatewayIntent) error
}

// AuthorizeEffect checks host-owned authorization without ever confusing
// the action class (issue) and concrete operation (create-account-token).
func (b *GatewayBinding) AuthorizeEffect(ctx context.Context, req GatewayIntent) error {
	if ctx == nil || ctx.Err() != nil || b == nil || b.Bridge == nil ||
		b.CheckCurrent == nil || b.RawTarget == "" || !sha256Hex.MatchString(b.PlanDigest) ||
		!sha256Hex.MatchString(b.GrantDigest) {
		return errors.New("trusted bound gateway host and live lease checker required")
	}
	id := b.Bridge.Identity
	action, mutation, err := ClassifyOperation(req.Provider, req.Operation)
	if err != nil || !mutation || action != req.Action {
		return errors.New("provider operation and authorization class differ")
	}
	if req.GoalID != id.GoalID || req.MutationID != id.OperationID ||
		req.Generation != id.WriterGeneration || req.Generation < 1 ||
		req.Owner != b.Owner || req.Session != b.Session ||
		req.PlanDigest != b.PlanDigest || req.GrantDigest != b.GrantDigest ||
		id.HeadSHA != b.ExpectedHead || b.ExpectedHead == "" ||
		!sha256Hex.MatchString(req.TargetDigest) ||
		req.TargetDigest != TargetDigest(req.Provider, req.Operation, b.RawTarget) {
		return errors.New("signed request differs from host-owned identity")
	}
	if err := b.CheckCurrent(ctx, req); err != nil {
		return fmt.Errorf("current Goal/Grant/lease check refused: %w", err)
	}
	return b.Bridge.Authorize(ctx, req.Provider, req.Operation, b.RawTarget)
}
