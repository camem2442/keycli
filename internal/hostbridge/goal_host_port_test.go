package hostbridge

import (
	"context"
	"testing"
)

func TestR0OriginalGoalPortUnavailableFailsClosed(t *testing.T) {
	p := &GoalHostPort{}
	req := GatewayIntent{GoalID: "g", MutationID: "m", Provider: "synthetic",
		Operation: "record-local-test", Action: ActionStage}
	if err := p.CheckCurrent(context.Background(), req); err == nil {
		t.Fatal("an unconfigured original Goal authority must never authorize")
	}
	if _, err := (GoalAdmitter{Port: p}).Admit(Request{}); err == nil {
		t.Fatal("a missing source and lease cannot authorize a Bridge")
	}
}
