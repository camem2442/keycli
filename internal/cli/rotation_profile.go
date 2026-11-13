package cli

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
)

var errRotationOperation = errors.New("unregistered rotation operation")

type rotationProfile struct {
	gatewayConfig  string
	sessionKeyFile string
	operations     map[string]effectgateway.Request
}

func rotationAbsolutePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/" && !strings.ContainsRune(path, 0)
}

// loadRotationProfile consumes the writer-facing projection of host registry
// entries. The Gateway revalidates the complete protected registration.
func loadRotationProfile(path string) (rotationProfile, error) {
	raw, err := effectgateway.ReadPrivateFile(path, 64<<10)
	if err != nil {
		return rotationProfile{}, err
	}
	var version int
	var goal, owner, session string
	var generation int64
	var entries []json.RawMessage
	profile := rotationProfile{operations: make(map[string]effectgateway.Request)}
	if !effectgateway.DecodePrivateConfig(raw, map[string]any{
		"v": &version, "gateway_config": &profile.gatewayConfig, "session_key_file": &profile.sessionKeyFile,
		"goal_id": &goal, "owner": &owner, "session": &session, "generation": &generation, "operations": &entries,
	}) || version != 1 || len(entries) == 0 || !rotationAbsolutePath(profile.gatewayConfig) ||
		!rotationAbsolutePath(profile.sessionKeyFile) {
		return rotationProfile{}, errRotationInput
	}
	mutations := make(map[string]bool)
	for _, entry := range entries {
		b := authoritybinding.Binding{GoalID: goal, Owner: owner, Session: session, WriterGeneration: generation}
		var providerOperation string
		if !effectgateway.DecodePrivateConfig(entry, map[string]any{
			"operation": &b.Operation, "mutation_id": &b.MutationID, "provider": &b.Provider,
			"provider_operation": &providerOperation, "resource_digest": &b.ResourceDigest,
			"target_digest": &b.TargetDigest, "plan_digest": &b.PlanDigest, "grant_digest": &b.GrantDigest,
		}) || !b.Valid() {
			return rotationProfile{}, errRotationInput
		}
		action, mutating, err := hostbridge.ClassifyOperation(b.Provider, providerOperation)
		if err != nil || !mutating || mutations[b.MutationID] {
			return rotationProfile{}, errRotationInput
		}
		if _, exists := profile.operations[b.Operation]; exists {
			return rotationProfile{}, errRotationInput
		}
		mutations[b.MutationID] = true
		profile.operations[b.Operation] = effectgateway.Request{
			GoalID: b.GoalID, MutationID: b.MutationID, Provider: b.Provider, Action: action,
			TargetDigest: b.TargetDigest, PlanDigest: b.PlanDigest, GrantDigest: b.GrantDigest,
			Owner: b.Owner, Session: b.Session, Generation: b.WriterGeneration,
		}
	}
	return profile, nil
}

func (p rotationProfile) operation(id string) (effectgateway.Request, error) {
	request, ok := p.operations[id]
	if !ok {
		return effectgateway.Request{}, errRotationOperation
	}
	return request, nil
}
