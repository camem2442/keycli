package githubdelivery

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/delivery"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/target/gh"
)

type writerFunc func(context.Context, gh.SecretRESTTarget, func(context.Context) ([]byte, error)) (gh.SecretWriteOutcome, error)

func (fn writerFunc) PutSecretFrom(ctx context.Context, target gh.SecretRESTTarget, load func(context.Context) ([]byte, error)) (gh.SecretWriteOutcome, error) {
	return fn(ctx, target, load)
}

func TestAdmissionAndUncertainGitHubWrite(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := journal.Store{Root: filepath.Join(root, "journal")}
	ctx := context.Background()
	planFor := func(ids string) delivery.DeliveryPlan {
		t.Helper()
		profiles, err := delivery.ParseProfiles([]byte(`schema_version=1
[profiles.fixture]
provider="github-actions-secret"
scope="organization"
organization="fixture"
effect="immediate-secret-update"
source="fixture"
destination="FIXTURE"
selected_repository_ids=[` + ids + `] `))
		if err != nil {
			t.Fatal(err)
		}
		meta, err := delivery.NewMetadataSnapshot(config.Config{}, "ci")
		if err != nil {
			t.Fatal(err)
		}
		plan, err := delivery.BuildPlan(profiles, "fixture", meta)
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	plan := planFor("11")
	var writes, loads int
	writer := writerFunc(func(ctx context.Context, target gh.SecretRESTTarget, load func(context.Context) ([]byte, error)) (gh.SecretWriteOutcome, error) {
		writes++
		rec, err := store.Status("first")
		if err != nil || rec.State != journal.UnknownEffect {
			t.Fatalf("effect ran without durable dispatch marker: %v %v", rec.State, err)
		}
		if target.OrganizationID != 9 || target.Destination != "FIXTURE" {
			t.Fatal("wrong target")
		}
		value, err := load(ctx)
		if err != nil || len(value) != 1 {
			t.Fatal("unexpected source")
		}
		return gh.SecretWriteUnknown, errors.New("fixture timeout")
	})
	load := func(_ context.Context, source delivery.SecretRef) ([]byte, error) {
		loads++
		if source.CanonicalName != "fixture" {
			t.Fatal("wrong source")
		}
		return []byte{1}, nil
	}
	reject := func(context.Context, string, delivery.DeliveryPlan) (Grant, error) {
		return Grant{}, errors.New("denied")
	}
	allow := func(_ context.Context, _ string, p delivery.DeliveryPlan) (Grant, error) {
		return Grant{PlanDigest: p.ID, GrantDigest: strings.Repeat("a", 64), OrganizationID: 9, ExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	if _, err := run(ctx, store, "denied", plan, reject, load, writer); err == nil {
		t.Fatal("denied grant accepted")
	}
	tampered := plan
	tampered.Target.Destination = "OTHER"
	if _, err := run(ctx, store, "tampered", tampered, allow, load, writer); err == nil {
		t.Fatal("tampered plan accepted")
	}
	if writes != 0 || loads != 0 {
		t.Fatal("work ran before admission")
	}
	record, err := run(ctx, store, "first", plan, allow, load, writer)
	if !errors.Is(err, journal.ErrUnknownEffect) || record.State != journal.UnknownEffect {
		t.Fatalf("uncertain outcome was not preserved: %v %v", record.State, err)
	}
	_, err = run(ctx, store, "second", planFor("11,12"), allow, load, writer)
	if !errors.Is(err, journal.ErrTargetPending) || writes != 1 || loads != 1 {
		t.Fatalf("changed organization selection bypassed latch: %v", err)
	}
}
