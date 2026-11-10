package rotation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

func rotationStore(t *testing.T) journal.Store {
	t.Helper()
	parent := t.TempDir()
	if e := os.Chmod(parent, 0700); e != nil {
		t.Fatal(e)
	}
	return journal.Store{Root: filepath.Join(parent, "delivery-journal")}
}

func planFixture(t *testing.T) PreparationPlan {
	t.Helper()
	old := Generation{Alias: "openai_worker", Provider: "cloudflare", Target: "fixture-worker", Kind: StaticServiceKey, Number: 1}
	next := old
	next.Number = 2
	p, err := NewPreparationPlan(old, next, []Consumer{{ID: "b", Required: true}, {ID: "a", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrepareExactPlanAndJournal(t *testing.T) {
	p := planFixture(t)
	called := 0
	digest := strings.Repeat("a", 64)
	result, err := Prepare(context.Background(), rotationStore(t), "op-prepare-1", p,
		func(context.Context, string, PreparationPlan) (PreparationGrant, error) {
			return PreparationGrant{PlanDigest: p.ID, GrantDigest: digest, ExpiresAt: time.Now().Add(time.Minute)}, nil
		},
		func(_ context.Context, got PreparationPlan) (journal.Observation, error) {
			called++
			if got.ID != p.ID {
				t.Fatal("plan changed")
			}
			return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
		})
	if err != nil || result.State != journal.Acknowledged || called != 1 || result.PlanDigest != p.ID {
		t.Fatalf("bad preparation %v %v", result, err)
	}
}

func TestPrepareDeniesForgedPlanBeforeEffects(t *testing.T) {
	p := planFixture(t)
	p.ID = "forged"
	called := false
	_, err := Prepare(context.Background(), rotationStore(t), "prepare", p, func(context.Context, string, PreparationPlan) (PreparationGrant, error) {
		called = true
		return PreparationGrant{}, nil
	}, func(context.Context, PreparationPlan) (journal.Observation, error) {
		called = true
		return journal.Observation{}, nil
	})
	if err == nil || called {
		t.Fatal("noncanonical plan admitted")
	}
	p = planFixture(t)
	p.Consumers[0].ID = "z"
	_, err = Prepare(context.Background(), rotationStore(t), "prepare", p, func(context.Context, string, PreparationPlan) (PreparationGrant, error) {
		called = true
		return PreparationGrant{}, nil
	}, func(context.Context, PreparationPlan) (journal.Observation, error) {
		called = true
		return journal.Observation{}, nil
	})
	if err == nil || called {
		t.Fatal("mutated plan admitted")
	}
}

func TestPrepareUnknownStageCannotBeSuccess(t *testing.T) {
	p := planFixture(t)
	outcome, err := Prepare(context.Background(), rotationStore(t), "prepare", p, func(context.Context, string, PreparationPlan) (PreparationGrant, error) {
		return PreparationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Minute)}, nil
	}, func(context.Context, PreparationPlan) (journal.Observation, error) {
		return journal.Observation{State: journal.Acknowledged, ProofDigest: strings.Repeat("b", 64)}, errors.New("lost result")
	})
	if !errors.Is(err, journal.ErrUnknownEffect) || outcome.State != journal.UnknownEffect {
		t.Fatalf("uncertain issue accepted: %+v %v", outcome, err)
	}
}

func TestPreparationExcludesWIFAndDuplicateConsumers(t *testing.T) {
	p := planFixture(t)
	p.New.Kind = "wif"
	if _, err := NewPreparationPlan(p.Old, p.New, p.Consumers); err == nil {
		t.Fatal("WIF rotated as static")
	}
	p = planFixture(t)
	if _, err := NewPreparationPlan(p.Old, p.New, []Consumer{{ID: "x", Required: true}, {ID: "x", Required: true}}); err == nil {
		t.Fatal("duplicate consumer")
	}
}
