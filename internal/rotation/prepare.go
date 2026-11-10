package rotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

// PreparationPlan is a metadata-only immutable proposal for one static service
// key generation. It never contains secret values or provider credentials.
type PreparationPlan struct {
	Version   int        `json:"v"`
	ID        string     `json:"plan_digest"`
	Old       Generation `json:"old"`
	New       Generation `json:"new"`
	Consumers []Consumer `json:"consumers"`
}

type PreparationGrant struct {
	PlanDigest  string
	GrantDigest string
	ExpiresAt   time.Time
}
type AuthorizePreparation func(context.Context, string, PreparationPlan) (PreparationGrant, error)
type StagePreparedGeneration func(context.Context, PreparationPlan) (journal.Observation, error)

func NewPreparationPlan(old, new Generation, consumers []Consumer) (PreparationPlan, error) {
	if !validGenerations(old, new) || len(consumers) == 0 || len(consumers) > 128 {
		return PreparationPlan{}, errors.New("invalid static credential generation plan")
	}
	seen := map[string]bool{}
	cc := slices.Clone(consumers)
	for _, c := range cc {
		if !namePattern.MatchString(c.ID) || !c.Required || seen[c.ID] {
			return PreparationPlan{}, errors.New("invalid required credential consumer")
		}
		seen[c.ID] = true
	}
	slices.SortFunc(cc, func(a, b Consumer) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	p := PreparationPlan{Version: 1, Old: old, New: new, Consumers: cc}
	p.ID = preparationDigest(p)
	return p, nil
}

func validGenerations(old, new Generation) bool {
	return old.Kind == StaticServiceKey && new.Kind == StaticServiceKey &&
		namePattern.MatchString(old.Alias) && namePattern.MatchString(old.Provider) && namePattern.MatchString(old.Target) &&
		old.Alias == new.Alias && old.Provider == new.Provider && old.Target == new.Target &&
		old.Number > 0 && new.Number > old.Number
}
func preparationDigest(plan PreparationPlan) string {
	plan.ID = ""
	data, _ := json.Marshal(plan)
	d := sha256.Sum256(data)
	return hex.EncodeToString(d[:])
}
func targetDigest(plan PreparationPlan) string {
	data, _ := json.Marshal(struct{ Provider, Target, Alias string }{plan.Old.Provider, plan.Old.Target, plan.Old.Alias})
	d := sha256.Sum256(data)
	return hex.EncodeToString(d[:])
}

// Prepare calls effect only from inside journal.Dispatch after an independently
// authenticated trusted-host grant. A provider ACK is not a consumer proof.
func Prepare(ctx context.Context, store journal.Store, id string, plan PreparationPlan, authorize AuthorizePreparation, stage StagePreparedGeneration) (journal.Record, error) {
	if authorize == nil || stage == nil || plan.Version != 1 || plan.ID == "" || !validGenerations(plan.Old, plan.New) ||
		len(plan.Consumers) == 0 || preparationDigest(plan) != plan.ID {
		return journal.Record{}, errors.New("invalid credential preparation plan")
	}
	canonical, err := NewPreparationPlan(plan.Old, plan.New, plan.Consumers)
	if err != nil || canonical.ID != plan.ID || !slices.EqualFunc(canonical.Consumers, plan.Consumers, func(a, b Consumer) bool { return a == b }) {
		return journal.Record{}, errors.New("noncanonical credential preparation plan")
	}
	grant, err := authorize(ctx, id, plan)
	if err != nil || grant.PlanDigest != plan.ID || !digestPattern.MatchString(grant.GrantDigest) || !time.Now().Before(grant.ExpiresAt) || ctx.Err() != nil {
		return journal.Record{}, errors.New("credential preparation not admitted")
	}
	ctx, cancel := context.WithDeadline(ctx, grant.ExpiresAt)
	defer cancel()
	intent := journal.Intent{OperationID: id, TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: grant.GrantDigest}
	return store.Dispatch(intent, func() (journal.Observation, error) {
		observation, err := stage(ctx, plan)
		if err != nil || observation.State != journal.Acknowledged || !digestPattern.MatchString(observation.ProofDigest) {
			return journal.Observation{}, journal.ErrUnknownEffect
		}
		return observation, nil
	})
}
