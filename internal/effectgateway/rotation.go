package effectgateway

import (
	"context"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
)

// RotationResolver selects a preauthorized typed action from trusted host
// configuration. A configured resolver owns all submissions to its Gateway.
type RotationResolver interface {
	ResolveRotation(context.Context, Request) (RotationAction, error)
}

type RotationAction interface {
	Intent() journal.Intent
	Plan() rotation.PreparationPlan
	Execute(context.Context, func(context.Context) error) (journal.Observation, error)
}

type fenceRejection string

func (r fenceRejection) Error() string { return string(r) }

func matchesRotation(action RotationAction, req Request) bool {
	switch action.(type) {
	case rotation.PrepareAction:
		if req.Action != hostbridge.ActionStage && req.Action != hostbridge.ActionIssue {
			return false
		}
	case rotation.FinalizeAction:
		if req.Action != hostbridge.ActionRevoke {
			return false
		}
	default:
		return false
	}
	intent, plan := action.Intent(), action.Plan()
	// The plan digest covers the rotation target. Authority separately binds
	// the canonical provider target, which uses the HostBridge digest format.
	return intent.OperationID == req.MutationID && intent.PlanDigest == req.PlanDigest &&
		intent.GrantDigest == req.GrantDigest && plan.Old.Provider == req.Provider
}
