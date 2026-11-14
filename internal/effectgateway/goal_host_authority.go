package effectgateway

import (
	"context"

	"github.com/camem2442/keycli/internal/hostbridge"
)

// GoalHostAuthority binds a synthetic submission to the original Goal host.
type GoalHostAuthority struct {
	Binding *hostbridge.GatewayBinding
}

func (a GoalHostAuthority) Bind(_ context.Context, req Request) (BoundAuthority, error) {
	return goalHostIntent{binding: a.Binding, intent: hostbridge.GatewayIntent(req)}, nil
}

type goalHostIntent struct {
	binding *hostbridge.GatewayBinding
	intent  hostbridge.GatewayIntent
}

func (b goalHostIntent) Preflight(ctx context.Context) error {
	return b.binding.AuthorizeEffect(ctx, b.intent)
}

func (b goalHostIntent) VerifyExecution(ctx context.Context) error {
	return b.binding.AuthorizeEffect(ctx, b.intent)
}
