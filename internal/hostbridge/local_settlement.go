package hostbridge

import (
	"context"
	"errors"
	"time"
)

// ErrSettlementUnknown means Authority may have settled the mutation, but
// the client did not obtain a validated terminal reply.
var ErrSettlementUnknown = errors.New("AUTHORITY_SETTLEMENT_UNKNOWN")

// MutationSettlement reports a terminal state observed by Authority for an
// exact binding. It conveys no execution authority.
type MutationSettlement struct {
	State          string
	LeaseExpiresAt time.Time
	RequestDigest  string
}

// Finish asks Authority to independently observe terminal evidence and settle
// the mutation. The request contains the binding and no proposed outcome.
func (a *LocalAdmitter) Finish(ctx context.Context, binding AuthorityBinding) (MutationSettlement, error) {
	reply, err := a.exchange(ctx, "finish", binding)
	if err != nil {
		return MutationSettlement{}, err
	}
	if reply.Status == "BLOCKED" {
		switch reply.Reason {
		case "AUTHORITY_UNAVAILABLE":
			return MutationSettlement{}, errors.Join(ErrSettlementUnknown, ErrLocalAuthorityUnavailable)
		case "JOURNAL_DB_MISMATCH", "NOT_RUNNING":
			// Startup can settle rows before reporting a journal mismatch,
			// and a concurrent settlement can change the row before completion.
			return MutationSettlement{}, ErrSettlementUnknown
		default:
			return MutationSettlement{}, ErrAdmissionRejected
		}
	}

	expiry, _ := parseWireTime(reply.LeaseExpiresAt)
	return MutationSettlement{State: *reply.MutationState, LeaseExpiresAt: expiry, RequestDigest: *reply.RequestDigest}, nil
}
