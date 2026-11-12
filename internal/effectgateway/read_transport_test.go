package effectgateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
)

func TestGatewayTransportRequiresSingleExactEnvelope(t *testing.T) {
	req := testRequest()
	key := make([]byte, minKeyBytes)
	raw, err := json.Marshal(Sign(req, key))
	if err != nil {
		t.Fatal(err)
	}
	valid := string(raw)
	for name, body := range map[string]string{
		"trailing":            valid + "{}",
		"duplicate top-level": strings.Replace(valid, `"signature":`, `"signature":"duplicate","signature":`, 1),
		"duplicate request":   strings.Replace(valid, `"owner":`, `"owner":"duplicate","owner":`, 1),
		"unknown":             strings.Replace(valid, `"owner":`, `"unknown":1,"owner":`, 1),
		"case variation":      strings.Replace(valid, `"owner":`, `"Owner":`, 1),
		"null":                `{"request":null,"signature":"x"}`,
		"too large":           valid + strings.Repeat(" ", MaxEnvelopeBytes),
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/submit", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
			(&Gateway{}).Handler(time.Second).ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status %d, body %s", w.Code, w.Body)
			}
		})
	}
}

func TestGatewayReadAuthenticatesCurrentSuccessorAndOriginalGrant(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	req := testRequest()
	binding := authoritybinding.Binding{GoalID: req.GoalID, MutationID: req.MutationID, WriterGeneration: req.Generation, Owner: req.Owner,
		Session: req.Session, Provider: req.Provider, Operation: "original", ResourceDigest: strings.Repeat("d", 64), TargetDigest: req.TargetDigest, PlanDigest: req.PlanDigest, GrantDigest: req.GrantDigest}
	intent := journal.Intent{OperationID: req.MutationID, TargetDigest: req.TargetDigest, PlanDigest: req.PlanDigest, GrantDigest: req.GrantDigest}
	if _, err := h.gw.Journal.DispatchBound(intent, binding, func(journal.DispatchGuard) (journal.Observation, error) {
		return journal.Observation{}, errors.New("synthetic lost response")
	}); !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatal(err)
	}

	claim := activeClaim()
	claim.Generation++
	claim.Owner, claim.Session, claim.State = "successor", "successor-session", "RECONCILING"
	claims := &fakeClaims{snapshot: claim}
	h.gw.Claims = claims
	ring := NewMemoryKeyRing(time.Minute, func() time.Time { return testNow })
	h.gw.Keys = ring
	keyFile := filepath.Join(sessionTestDir(t), "successor.key")
	if err := ring.IssueClaim(ctx, claims, claim, keyFile); err != nil {
		t.Fatal(err)
	}
	key, err := ReadSessionKeyFile(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	identity := strings.Repeat("9", 64)
	grant := RecoveryGrant{Binding: binding, GrantDigest: strings.Repeat("8", 64), ProviderIdentityDigest: identity, ExpiresAt: testNow.Add(time.Minute)}
	read := ReadRequest{GoalID: req.GoalID, MutationID: req.MutationID, Owner: claim.Owner, Session: claim.Session, Generation: claim.Generation, ExpiresAt: testNow.Add(time.Minute), Action: ReadStatus}
	statusKey := readGrantKey{read.GoalID, read.MutationID, read.Owner, read.Session, ReadStatus, read.Generation}
	reconcileKey := statusKey
	reconcileKey.Action = ReadReconcile
	h.gw.ReadAuthority = readGrantRegistry{statusKey: grant}
	observations := 0
	changeDuringObservation := false
	h.gw.RecoveryObserver = recoveryObserverFunc(func(_ context.Context, actual authoritybinding.Binding) (RecoveryEvidence, error) {
		observations++
		if changeDuringObservation {
			claims.snapshot.State = "DRAINING"
		}
		if actual != binding {
			t.Fatal("recovery targeted a different journal binding")
		}
		return RecoveryEvidence{State: journal.ObservedApplied, ObservedAt: testNow, ProviderIdentityDigest: identity, SourceDigest: strings.Repeat("7", 64)}, nil
	})
	exchange := func(route string, env ReadEnvelope) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(env)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, route, strings.NewReader(string(body)))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.gw.Handler(time.Second).ServeHTTP(w, r)
		return w
	}
	if w := exchange("/status", SignRead(read, key)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), StatusUnknown) {
		t.Fatalf("status: %d %s", w.Code, w.Body)
	}
	expired := grant
	expired.ExpiresAt = testNow
	h.gw.ReadAuthority = readGrantRegistry{statusKey: expired}
	if w := exchange("/status", SignRead(read, key)); w.Code != http.StatusForbidden {
		t.Fatalf("expired status grant authorized: %d %s", w.Code, w.Body)
	}
	h.gw.ReadAuthority = readGrantRegistry{statusKey: grant}
	forged := SignRead(read, key)
	forged.Request.Action = ReadReconcile
	if w := exchange("/reconcile", forged); w.Code != http.StatusUnauthorized {
		t.Fatalf("action not signed: %d %s", w.Code, w.Body)
	}
	read.Action = ReadReconcile
	if w := exchange("/reconcile", SignRead(read, key)); w.Code != http.StatusForbidden || observations != 0 {
		t.Fatalf("status grant authorized reconcile: %d %s", w.Code, w.Body)
	}
	h.gw.ReadAuthority = readGrantRegistry{statusKey: grant, reconcileKey: grant}
	changeDuringObservation = true
	if w := exchange("/reconcile", SignRead(read, key)); w.Code != http.StatusConflict {
		t.Fatalf("claim change did not stop settlement: %d %s", w.Code, w.Body)
	}
	record, err := h.gw.Journal.Status(binding.MutationID)
	if err != nil || record.State != journal.UnknownEffect {
		t.Fatalf("claim changed before settlement: %+v %v", record, err)
	}
	claims.snapshot = claim
	changeDuringObservation = false
	if w := exchange("/reconcile", SignRead(read, key)); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), string(journal.ObservedApplied)) || observations != 2 {
		t.Fatalf("reconcile: %d %s", w.Code, w.Body)
	}
	h.gw.ReadAuthority = readGrantRegistry{statusKey: grant}
	if w := exchange("/reconcile", SignRead(read, key)); w.Code != http.StatusForbidden || observations != 2 {
		t.Fatalf("terminal record bypassed grant: %d %s", w.Code, w.Body)
	}

	req.Owner, req.Session, req.Generation = claim.Owner, claim.Session, claim.Generation
	if _, err := h.gw.Submit(ctx, Sign(req, key)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("recovery key submitted an effect: %v", err)
	}
	read.Action = ReadStatus
	claims.snapshot.State = "ACTIVE"
	if w := exchange("/status", SignRead(read, key)); w.Code != http.StatusUnauthorized {
		t.Fatalf("role change did not invalidate key: %d %s", w.Code, w.Body)
	}
}

func TestLoadReadAuthorityRejectsBindingSubstitution(t *testing.T) {
	req := testRequest()
	b := authoritybinding.Binding{GoalID: req.GoalID, MutationID: req.MutationID, WriterGeneration: req.Generation, Owner: req.Owner, Session: req.Session,
		Provider: req.Provider, Operation: "original", ResourceDigest: strings.Repeat("d", 64), TargetDigest: req.TargetDigest, PlanDigest: req.PlanDigest, GrantDigest: req.GrantDigest}
	path := filepath.Join(sessionTestDir(t), "read-grants.json")
	entry := map[string]any{"binding": b, "owner": "successor", "session": "successor-session", "generation": b.WriterGeneration + 1, "action": ReadStatus,
		"grant_digest": strings.Repeat("8", 64), "provider_identity_digest": strings.Repeat("9", 64), "expires_at": time.Now().Add(time.Minute)}
	raw, err := json.Marshal(map[string]any{"v": 1, "grants": []any{entry}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	a, err := LoadReadAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	read := ReadRequest{GoalID: b.GoalID, MutationID: b.MutationID, Owner: "successor", Session: "successor-session", Generation: b.WriterGeneration + 1, Action: ReadStatus}
	if _, err := a.AuthorizeRead(context.Background(), read, b); err != nil {
		t.Fatal(err)
	}
	entry["expires_at"] = time.Now().Add(-time.Minute)
	expired, err := json.Marshal(map[string]any{"v": 1, "grants": []any{entry}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, expired, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReadAuthority(path); err != nil {
		t.Fatalf("expired historical grant blocked startup: %v", err)
	}
	b.ResourceDigest = strings.Repeat("1", 64)
	if _, err := a.AuthorizeRead(context.Background(), read, b); !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("different original binding allowed: %v", err)
	}
}

func TestOperationalReplayComparesOriginalIntentForSuccessor(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	reg, registered := authorityRegistration()
	binding := reg.Binding()
	req := Request{GoalID: binding.GoalID, MutationID: binding.MutationID, Provider: binding.Provider, Action: registered.Action,
		TargetDigest: binding.TargetDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest,
		Generation: binding.WriterGeneration + 1, Owner: "successor", Session: "successor-session"}
	intent := journal.Intent{OperationID: binding.MutationID, TargetDigest: binding.TargetDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest}
	_, err := h.gw.Journal.DispatchBound(intent, binding, func(journal.DispatchGuard) (journal.Observation, error) {
		return journal.Observation{}, errors.New("lost reply")
	})
	if !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatal(err)
	}
	h.gw.Authority = LocalAuthority{Registry: hostRegistry{{binding.GoalID, binding.MutationID}: reg}}
	if duplicate, err := h.gw.operationalReplay(ctx, req); !duplicate || err != nil {
		t.Fatalf("same original intent: duplicate=%t, %v", duplicate, err)
	}
	req.PlanDigest = strings.Repeat("6", 64)
	if duplicate, err := h.gw.operationalReplay(ctx, req); duplicate || !errors.Is(err, ErrMutationConflict) {
		t.Fatalf("changed intent: duplicate=%t, %v", duplicate, err)
	}
	req.GoalID = "different-goal"
	if duplicate, err := h.gw.operationalReplay(ctx, req); duplicate || !errors.Is(err, ErrAdmissionRejected) {
		t.Fatalf("different goal: duplicate=%t, %v", duplicate, err)
	}
}
