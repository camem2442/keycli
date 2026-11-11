package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/rotation"
	"golang.org/x/sys/unix"
)

const rotationInputFixture = `{
	"v": 1,
	"old": {"alias":"deploy","kind":"static-service-key","provider":"example","target":"fixture","number":1},
	"new": {"alias":"deploy","kind":"static-service-key","provider":"example","target":"fixture","number":2},
	"required_consumers": ["worker", "api"]
}`

func writeRotationInputFixture(t *testing.T, data string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rotation.json")
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildRotationPlanInput(t *testing.T) {
	plan, err := buildRotationPlan(writeRotationInputFixture(t, rotationInputFixture, 0600))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != 1 || !journal.ValidDigest(plan.ID) || plan.Old.Number != 1 || plan.New.Number != 2 ||
		!reflect.DeepEqual(plan.Consumers, []rotation.Consumer{{ID: "api", Required: true}, {ID: "worker", Required: true}}) {
		t.Fatalf("unexpected canonical plan: %#v", plan)
	}
	reordered := strings.Replace(rotationInputFixture, `"worker", "api"`, `"api", "worker"`, 1)
	other, err := buildRotationPlan(writeRotationInputFixture(t, reordered, 0400))
	if err != nil || !reflect.DeepEqual(plan, other) {
		t.Fatalf("reordered input changed plan: %#v, %v", other, err)
	}
}

func TestBuildRotationPlanRejectsMalformedInput(t *testing.T) {
	for name, input := range map[string]string{
		"unknown root field":     strings.Replace(rotationInputFixture, `"v": 1`, `"v": 1, "extra": 1`, 1),
		"duplicate root field":   strings.Replace(rotationInputFixture, `"v": 1`, `"v": 1, "v": 1`, 1),
		"unknown nested field":   strings.Replace(rotationInputFixture, `"number":1`, `"number":1,"extra":1`, 1),
		"duplicate nested field": strings.Replace(rotationInputFixture, `"number":1`, `"number":1,"number":1`, 1),
		"wrong field case":       strings.Replace(rotationInputFixture, `"number":1`, `"Number":1`, 1),
		"missing field":          strings.Replace(rotationInputFixture, `"target":"fixture",`, "", 1),
		"null field":             strings.Replace(rotationInputFixture, `"number":1`, `"number":null`, 1),
		"wrong version":          strings.Replace(rotationInputFixture, `"v": 1`, `"v": 2`, 1),
		"same generation":        strings.Replace(rotationInputFixture, `"number":2`, `"number":1`, 1),
		"mismatched target":      strings.Replace(rotationInputFixture, `"target":"fixture"`, `"target":"other"`, 1),
		"non-static kind":        strings.Replace(rotationInputFixture, `"static-service-key"`, `"workload-token"`, 1),
		"duplicate consumers":    strings.Replace(rotationInputFixture, `"worker", "api"`, `"api", "api"`, 1),
		"empty consumers":        strings.Replace(rotationInputFixture, `"worker", "api"`, "", 1),
		"null consumer":          strings.Replace(rotationInputFixture, `"worker", "api"`, `"api", null`, 1),
		"trailing input":         rotationInputFixture + `{}`,
		"invalid utf8":           rotationInputFixture + "\xff",
		"oversized":              rotationInputFixture + strings.Repeat(" ", maxRotationInputBytes),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := buildRotationPlan(writeRotationInputFixture(t, input, 0600))
			if !errors.Is(err, errRotationInput) {
				t.Fatalf("expected invalid plan input error, got %v", err)
			}
		})
	}
}

func TestBuildRotationPlanRejectsUnsafeFile(t *testing.T) {
	target := writeRotationInputFixture(t, rotationInputFixture, 0600)
	dir := t.TempDir()
	symlink := filepath.Join(dir, "symlink")
	if err := os.Symlink(target, symlink); err != nil {
		t.Fatal(err)
	}
	hardlink := filepath.Join(dir, "hardlink")
	if err := os.Link(target, hardlink); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(dir, "pipe")
	if err := unix.Mkfifo(pipe, 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		writeRotationInputFixture(t, rotationInputFixture, 0644),
		filepath.Join(dir, "missing"), dir, symlink, hardlink, target, pipe,
	} {
		if _, err := buildRotationPlan(path); err == nil || errors.Is(err, errRotationInput) {
			t.Fatalf("expected file access error for %s, got %v", filepath.Base(path), err)
		}
	}
}

func TestRotationStatusUsesLocalUnverifiedProjection(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := journal.Store{Root: filepath.Join(parent, "journal")}
	plan, err := buildRotationPlan(writeRotationInputFixture(t, rotationInputFixture, 0600))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadRotationStatus(ctx, store.Root, plan.ID, now); !errors.Is(err, journal.ErrNotFound) {
		t.Fatalf("expected missing session, got %v", err)
	}
	if _, err := os.Stat(store.Root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing session lookup changed journal root: %v", err)
	}

	digest := strings.Repeat("a", 64)
	intent := func(id string) journal.Intent {
		return journal.Intent{OperationID: id, PlanDigest: plan.ID, TargetDigest: digest, GrantDigest: digest}
	}
	stage, activate, revoke, rollback := intent("fixture-stage"), intent("fixture-activate"), intent("fixture-revoke"), intent("fixture-rollback")
	session := rotation.Session{
		Version: 1, Plan: plan, Stage: stage, OldIdentityDigest: digest,
		NewIdentityDigest: strings.Repeat("b", 64), StageEvidence: digest,
		Activate: &activate, Revoke: &revoke, Rollback: &rollback, RevocationEvidence: digest,
		Activation: &rotation.ActivationEvidence{
			StageReceiptDigest: digest, PromotionReceiptDigest: digest,
			ActivatedAt: now.Add(-10 * time.Minute), GraceUntil: now.Add(-5 * time.Minute),
		},
		Consumers: []rotation.ConsumerEvidence{{
			ConsumerID: "api", Generation: plan.New.Number, ObservedAt: now.Add(-time.Minute), EvidenceDigest: digest,
		}},
	}
	if _, err := rotation.SaveSession(store, rotation.SessionSnapshot{}, session); err != nil {
		t.Fatal(err)
	}
	check := func(want rotation.SessionState) RotationStatus {
		t.Helper()
		status, err := loadRotationStatus(ctx, store.Root, plan.ID, now)
		if err != nil || status.State != want {
			t.Fatalf("status state = %s, want %s, error = %v", status.State, want, err)
		}
		return status
	}
	ack := func(op journal.Intent) {
		t.Helper()
		if _, err := store.Dispatch(op, func() (journal.Observation, error) {
			return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	check(rotation.SessionPlanned)
	ack(stage)
	ack(activate)
	status := check(rotation.SessionPreparedUnverified)
	if status.Version != 1 || status.PlanDigest != plan.ID || status.Grace == nil || status.Grace.Verified ||
		!status.Grace.ActivatedAt.Equal(session.Activation.ActivatedAt) || !status.Grace.GraceUntil.Equal(session.Activation.GraceUntil) ||
		!reflect.DeepEqual(status.RequiredConsumers, []string{"api", "worker"}) ||
		!reflect.DeepEqual(status.MissingConsumers, []string{"worker"}) ||
		!reflect.DeepEqual(status.UnverifiedConsumers, []string{"api"}) {
		t.Fatalf("unexpected status: %#v", status)
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{digest, session.NewIdentityDigest, stage.OperationID, plan.Old.Target, plan.Old.Alias} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("status exposed raw session metadata: %s", raw)
		}
	}

	ack(revoke)
	check(rotation.SessionRevokeAcknowledged)
	if _, err := store.Dispatch(rollback, func() (journal.Observation, error) {
		return journal.Observation{}, errors.New("fixture interrupted")
	}); !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatal(err)
	}
	check(rotation.SessionUnknownEffect)
}
