package hostbridge

import (
	"context"
	"errors"
	"regexp"
	"time"
)

// ErrReservationUnknown means Authority may have reserved the mutation, but
// the client did not obtain a validated reply.
var ErrReservationUnknown = errors.New("AUTHORITY_RESERVATION_UNKNOWN")

var wireDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

// MutationReservation reports a new reservation or the state of an exact
// duplicate. The receipt conveys no execution authority.
type MutationReservation struct {
	Reason          string
	State           string
	LeaseExpiresAt  time.Time
	AdmissionDigest string
	RequestDigest   string
}

// Reserve asks Authority to admit an exact mutation. A v1 advisory-only
// service returns ErrExecutionFenceUnavailable without a reservation.
func (a *LocalAdmitter) Reserve(ctx context.Context, binding AuthorityBinding) (MutationReservation, error) {
	reply, err := a.exchange(ctx, "admit", binding)
	if err != nil {
		return MutationReservation{}, err
	}
	switch reply.Status {
	case "ADVISORY":
		return MutationReservation{}, ErrExecutionFenceUnavailable
	case "BLOCKED":
		// The coordinator can persist a reservation before spending budget or
		// before an error is reported by the server.
		switch reply.Reason {
		case "BUDGET_UNVERIFIED":
			return MutationReservation{}, ErrReservationUnknown
		case "AUTHORITY_UNAVAILABLE":
			return MutationReservation{}, errors.Join(ErrReservationUnknown, ErrLocalAuthorityUnavailable)
		default:
			return MutationReservation{}, ErrAdmissionRejected
		}
	}

	expiry, _ := parseWireTime(reply.LeaseExpiresAt)
	result := MutationReservation{
		Reason: reply.Reason, State: *reply.MutationState, LeaseExpiresAt: expiry, RequestDigest: *reply.RequestDigest,
	}
	if reply.AdmissionDigest != nil {
		result.AdmissionDigest = *reply.AdmissionDigest
	}
	return result, nil
}

func validReservationReply(req authorityRequest, reply authorityReply) bool {
	if reply.Status != "OK" || reply.ExpiresAt != nil || reply.MutationState == nil ||
		reply.RequestDigest == nil || *reply.RequestDigest != jsonDigest(req.AuthorityBinding) {
		return false
	}
	if _, ok := parseWireTime(reply.LeaseExpiresAt); !ok {
		return false
	}
	switch reply.Reason {
	case "ADMITTED":
		return *reply.MutationState == "RUNNING" && reply.AdmissionDigest != nil && wireDigest.MatchString(*reply.AdmissionDigest)
	case "DUPLICATE":
		if reply.AdmissionDigest == nil {
			switch *reply.MutationState {
			case "RUNNING", "DONE", "FAILED", "ORPHANED", "RECOVERED":
				return true
			}
		}
	}
	return false
}
