package owner2fa

import "context"

// BoundMFAChild ties an independently admitted 2FA operation to one parent
// browser-login request. It does not create, sign or approve grants: Inner
// MUST be a trusted owner-host original Goal/Grant/Claim/journal verifier.
type BoundMFAChild struct {
	ParentRequestID string
	ChildRequestID  string
	Site            string
	Inner           Authority
}

// VerifyAndReserve refuses parent-ID reuse, cross-site requests, and missing
// original authority before any seed is fetched. It forwards only the exact
// child Challenge to the independently supplied owner-host verifier.
func (b *BoundMFAChild) VerifyAndReserve(ctx context.Context, c Challenge) error {
	if b == nil || ctx.Err() != nil || b.Inner == nil ||
		!validID(b.ParentRequestID) || !validID(b.ChildRequestID) ||
		b.ParentRequestID == b.ChildRequestID || !validSite(b.Site) ||
		c.Factor != BrokerTOTP || c.Site != b.Site ||
		c.RequestID != b.ChildRequestID || c.ParentRequestID != b.ParentRequestID {
		return ErrDenied
	}
	if b.Inner.VerifyAndReserve(ctx, c) != nil {
		return ErrDenied
	}
	return nil
}
