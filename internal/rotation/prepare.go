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

// PrepareAction binds a validated plan to a host grant and a staging callback.
// The caller must execute it inside the single durable journal dispatch.
type PrepareAction struct {
	intent    journal.Intent
	plan      PreparationPlan
	expiresAt time.Time
	stage     StagePreparedGeneration
}

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
	action, err := NewPrepareAction(ctx, id, plan, authorize, stage)
	if err != nil {
		return journal.Record{}, err
	}
	return dispatchAction(store, action.Intent(), func() (journal.Observation, error) {
		return action.Execute(ctx, nil)
	})
}

// NewPrepareAction validates and admits a preparation without staging it.
func NewPrepareAction(ctx context.Context, id string, plan PreparationPlan, authorize AuthorizePreparation, stage StagePreparedGeneration) (PrepareAction, error) {
	if authorize == nil || stage == nil || plan.Version != 1 || plan.ID == "" || !validGenerations(plan.Old, plan.New) ||
		len(plan.Consumers) == 0 || preparationDigest(plan) != plan.ID || ctx == nil || ctx.Err() != nil {
		return PrepareAction{}, errors.New("invalid credential preparation plan")
	}
	canonical, err := NewPreparationPlan(plan.Old, plan.New, plan.Consumers)
	if err != nil || canonical.ID != plan.ID || !slices.Equal(canonical.Consumers, plan.Consumers) {
		return PrepareAction{}, errors.New("noncanonical credential preparation plan")
	}
	grant, err := authorize(ctx, id, clonePreparationPlan(canonical))
	if err != nil || grant.PlanDigest != plan.ID || !digestPattern.MatchString(grant.GrantDigest) || !time.Now().Before(grant.ExpiresAt) || ctx.Err() != nil {
		return PrepareAction{}, errors.New("credential preparation not admitted")
	}
	intent := journal.Intent{OperationID: id, TargetDigest: targetDigest(canonical), PlanDigest: canonical.ID, GrantDigest: grant.GrantDigest}
	return PrepareAction{intent: intent, plan: canonical, expiresAt: grant.ExpiresAt, stage: stage}, nil
}

func (a PrepareAction) Intent() journal.Intent { return a.intent }

// Plan returns a copy of the admitted metadata for trusted target binding.
func (a PrepareAction) Plan() PreparationPlan { return clonePreparationPlan(a.plan) }

// Execute stages the admitted generation. beforeEffect may enforce a caller's
// execution fence immediately before the provider callback.
func (a PrepareAction) Execute(ctx context.Context, beforeEffect func(context.Context) error) (journal.Observation, error) {
	if a.stage == nil || !actionCurrent(ctx, a.expiresAt) {
		return journal.Observation{}, ErrActionNotCurrent
	}
	ctx, cancel := context.WithDeadline(ctx, a.expiresAt)
	defer cancel()
	if err := verifyActionBoundary(ctx, a.expiresAt, beforeEffect); err != nil {
		return journal.Observation{}, err
	}
	return acknowledged(a.stage(ctx, clonePreparationPlan(a.plan)))
}

func clonePreparationPlan(plan PreparationPlan) PreparationPlan {
	plan.Consumers = slices.Clone(plan.Consumers)
	return plan
}
