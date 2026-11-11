package journal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/authoritybinding"
)

func TestMigratePendingV1PreservesOriginalAndResourceFence(t *testing.T) {
	store := newTestStore(t)
	legacy := testIntent("legacy-mapping")
	if _, err := store.Dispatch(legacy, func() (Observation, error) {
		return Observation{}, errors.New("response lost")
	}); !errors.Is(err, ErrUnknownEffect) {
		t.Fatal(err)
	}
	path := filepath.Join(store.Root, legacy.OperationID+".json")
	backup := filepath.Join(store.Root, "."+legacy.OperationID+".v1")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	binding := authoritybinding.Binding{
		GoalID: "goal", MutationID: legacy.OperationID, WriterGeneration: 1, Owner: "owner",
		Session: "session", Provider: "cloudflare", Operation: "rotate", ResourceDigest: strings.Repeat("f", 64),
		TargetDigest: legacy.TargetDigest, PlanDigest: legacy.PlanDigest, GrantDigest: legacy.GrantDigest,
	}
	verify := func(intent Intent) (authoritybinding.Binding, error) {
		if intent != legacy {
			t.Fatalf("mapping saw incorrect intent: %+v", intent)
		}
		return binding, nil
	}
	// A failed backup publication must leave the original bytes untouched.
	if err := os.Mkdir(backup, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MigratePendingV1(legacy.OperationID, verify); err == nil {
		t.Fatal("migration accepted an unusable backup destination")
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("failed migration changed the original: %v", err)
	}
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		record, err := store.MigratePendingV1(legacy.OperationID, verify)
		if err != nil || record.Version != 3 || record.State != UnknownEffect || record.Binding == nil ||
			*record.Binding != binding || record.ResourceDigest != binding.ResourceDigest {
			t.Fatalf("mapped record: %+v, %v", record, err)
		}
	}
	if data, err := os.ReadFile(backup); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("archived bytes differ: %v", err)
	}
	next := testIntent("mapped-alias")
	next.TargetDigest = strings.Repeat("a", 64)
	binding.MutationID, binding.TargetDigest = next.OperationID, next.TargetDigest
	if _, err := store.DispatchBound(next, binding, neverBoundEffect(t)); !errors.Is(err, ErrTargetPending) {
		t.Fatalf("mapped resource did not fence alias: %v", err)
	}
}
