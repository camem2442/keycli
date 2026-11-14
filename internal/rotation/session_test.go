package rotation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
)

func TestSessionReloadAndDerivedState(t *testing.T) {
	ctx := context.Background()
	store := rotationStore(t)
	plan := planFixture(t)
	now := time.Now()
	digest := strings.Repeat("e", 64)
	intent := func(id string) journal.Intent {
		return journal.Intent{OperationID: id, TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: digest}
	}
	s := Session{Version: 1, Plan: plan, OldIdentityDigest: digest, Stage: intent("stage")}
	snapshot, err := SaveSession(store, SessionSnapshot{}, s)
	if err != nil {
		t.Fatal(err)
	}
	verified := SessionVerifiers{
		Stage:      func(context.Context, Session, journal.Record) error { return nil },
		Activation: verifyActivationOK, Consumer: verifyConsumerOK,
	}
	check := func(want SessionState) {
		t.Helper()
		reopened, err := LoadSession(journal.Store{Root: store.Root}, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		state, err := reopened.Session.State(ctx, store, now, verified)
		if err != nil || state != want {
			t.Fatalf("state=%s want=%s error=%v", state, want, err)
		}
	}
	save := func() {
		t.Helper()
		updated, err := SaveSession(store, snapshot, s)
		if err != nil {
			t.Fatal(err)
		}
		snapshot = updated
	}
	ack := func(op journal.Intent) {
		t.Helper()
		if _, err := store.Dispatch(op, func() (journal.Observation, error) {
			return journal.Observation{State: journal.Acknowledged, ProofDigest: digest}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	check(SessionPlanned)
	ack(s.Stage)
	check(SessionPreparedUnverified)
	stale := snapshot
	s.NewIdentityDigest = strings.Repeat("f", 64)
	a := activationFixture(now)
	s.StageEvidence = a.StageReceiptDigest
	save()
	if _, err := SaveSession(store, stale, stale.Session); !errors.Is(err, journal.ErrMetadataChanged) {
		t.Fatalf("stale update: %v", err)
	}
	check(SessionStageVerified)
	activate := intent("activate")
	s.Activate, s.Activation = &activate, &a
	save()
	ack(activate)
	check(SessionActiveVerified)
	s.Consumers = evidenceFixture(now)
	s.Consumers[0].ObservedAt = now.Add(-16 * time.Minute)
	save()
	check(SessionActiveVerified)
	s.Consumers = evidenceFixture(now)
	a.GraceUntil = now.Add(time.Second)
	save()
	check(SessionActiveVerified)
	now = a.GraceUntil
	check(SessionRetirementReady)
	revoke := intent("revoke")
	s.Revoke, s.RevocationEvidence = &revoke, digest
	save()
	ack(revoke)
	verified.Revocation = func(context.Context, Session, journal.Record) error {
		return errors.New("old credential still active")
	}
	check(SessionRevokeAcknowledged)
	verified.Revocation = func(context.Context, Session, journal.Record) error { return nil }
	check(SessionRevokeVerified)
}

func TestSessionRetainsUnknownEffectAfterRestart(t *testing.T) {
	store := rotationStore(t)
	plan := planFixture(t)
	digest := strings.Repeat("e", 64)
	stage := journal.Intent{OperationID: "stage-lost", TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: digest}
	if _, err := SaveSession(store, SessionSnapshot{}, Session{Version: 1, Plan: plan, OldIdentityDigest: digest, Stage: stage}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Dispatch(stage, func() (journal.Observation, error) {
		return journal.Observation{}, errors.New("lost acknowledgement")
	}); !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatal(err)
	}
	reopened, err := LoadSession(store, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state, err := reopened.Session.State(context.Background(), store, time.Now(), SessionVerifiers{}); err != nil || state != SessionUnknownEffect {
		t.Fatalf("unknown stage after restart: %s, %v", state, err)
	}
}

func TestSessionSurvivesKilledDispatch(t *testing.T) {
	const rootEnv, planEnv = "KEYCLI_ROTATION_CRASH_ROOT", "KEYCLI_ROTATION_CRASH_PLAN"
	if root := os.Getenv(rootEnv); root != "" {
		store := journal.Store{Root: root}
		snapshot, err := LoadSession(store, os.Getenv(planEnv))
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.Dispatch(snapshot.Session.Stage, func() (journal.Observation, error) {
			fmt.Println("rotation-dispatched")
			for {
				time.Sleep(time.Second)
			}
		})
		t.Fatalf("dispatch never entered its callback: %v", err)
	}
	store := rotationStore(t)
	plan := planFixture(t)
	digest := strings.Repeat("e", 64)
	stage := journal.Intent{OperationID: "stage-killed", TargetDigest: targetDigest(plan), PlanDigest: plan.ID, GrantDigest: digest}
	if _, err := SaveSession(store, SessionSnapshot{}, Session{Version: 1, Plan: plan, OldIdentityDigest: digest, Stage: stage}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSessionSurvivesKilledDispatch$")
	child.Env = append(os.Environ(), rootEnv+"="+store.Root, planEnv+"="+plan.ID)
	child.Stderr = os.Stderr
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child.ProcessState == nil {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
	}()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "rotation-dispatched\n" {
		t.Fatalf("child did not reach dispatch: %q, %v", line, err)
	}
	check := func() {
		t.Helper()
		snapshot, err := LoadSession(store, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		state, err := snapshot.Session.State(ctx, store, time.Now(), SessionVerifiers{})
		if err != nil || state != SessionUnknownEffect {
			t.Fatalf("state=%s error=%v", state, err)
		}
	}
	check()
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil {
		t.Fatal("dispatch child exited normally")
	}
	check()
	if _, err := store.Dispatch(stage, func() (journal.Observation, error) {
		t.Fatal("killed dispatch was replayed")
		return journal.Observation{}, nil
	}); !errors.Is(err, journal.ErrDuplicate) {
		t.Fatalf("replay: %v", err)
	}
}
