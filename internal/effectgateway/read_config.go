package effectgateway

import (
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/camem2442/keycli/internal/authoritybinding"
)

var errReadConfig = errors.New("invalid or unsafe host read grant configuration")

type readGrantKey struct {
	GoalID, MutationID, Owner, Session, Action string
	Generation                                 int64
}

type readGrantRegistry map[readGrantKey]RecoveryGrant

func (r readGrantRegistry) AuthorizeRead(ctx context.Context, req ReadRequest, binding authoritybinding.Binding) (RecoveryGrant, error) {
	key := readGrantKey{req.GoalID, req.MutationID, req.Owner, req.Session, req.Action, req.Generation}
	grant, ok := r[key]
	if !ok || ctx.Err() != nil || grant.Binding != binding {
		return RecoveryGrant{}, ErrAdmissionRejected
	}
	return grant, nil
}

// LoadReadAuthority loads separately registered, owner-private grants for exact
// current sessions and the original immutable journal bindings.
func LoadReadAuthority(path string) (ReadAuthorizer, error) {
	raw, err := ReadPrivateFile(path, maxAuthorityConfig)
	if err != nil || !utf8.Valid(raw) {
		return nil, errReadConfig
	}
	var version int
	var items []json.RawMessage
	if !decodeAuthorityObject(raw, map[string]any{"v": &version, "grants": &items}, nil) || version != 1 || len(items) == 0 {
		return nil, errReadConfig
	}
	registry := make(readGrantRegistry, len(items))
	for _, item := range items {
		var key readGrantKey
		var grant RecoveryGrant
		var original json.RawMessage
		if !decodeAuthorityObject(item, map[string]any{
			"owner": &key.Owner, "session": &key.Session, "generation": &key.Generation, "action": &key.Action,
			"binding": &original, "grant_digest": &grant.GrantDigest,
			"provider_identity_digest": &grant.ProviderIdentityDigest, "expires_at": &grant.ExpiresAt,
		}, nil) || (key.Action != ReadStatus && key.Action != ReadReconcile) {
			return nil, errReadConfig
		}
		b := &grant.Binding
		if !decodeAuthorityObject(original, map[string]any{
			"goal_id": &b.GoalID, "mutation_id": &b.MutationID, "writer_generation": &b.WriterGeneration,
			"owner": &b.Owner, "session": &b.Session, "provider": &b.Provider, "operation": &b.Operation,
			"resource_digest": &b.ResourceDigest, "target_digest": &b.TargetDigest, "plan_digest": &b.PlanDigest, "grant_digest": &b.GrantDigest,
		}, nil) || !b.Valid() || !validRecoveryGrantBinding(grant, *b) || key.Generation < b.WriterGeneration ||
			(key.Generation == b.WriterGeneration && (key.Owner != b.Owner || key.Session != b.Session)) {
			return nil, errReadConfig
		}
		// Apply the same identifier restrictions to the successor as the
		// original binding, without copying it into the journal record.
		successor := *b
		successor.Owner, successor.Session, successor.WriterGeneration = key.Owner, key.Session, key.Generation
		if !successor.Valid() {
			return nil, errReadConfig
		}
		key.GoalID, key.MutationID = b.GoalID, b.MutationID
		if _, exists := registry[key]; exists {
			return nil, errReadConfig
		}
		registry[key] = grant
	}
	return registry, nil
}
