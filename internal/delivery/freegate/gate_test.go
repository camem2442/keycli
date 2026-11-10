package freegate

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

func gateFixture(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey, Evidence) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	e := Evidence{Version: 1, AccountID: strings.Repeat("a", 32), WorkerName: "kc-v09-stage-pilot-test1", Operation: "bootstrap", Plan: "workers-free", Origin: "owner-dashboard-attestation", ObservedUnix: now.Add(-time.Minute).Unix(), ExpiresUnix: now.Add(10 * time.Minute).Unix(), Workers: []string{"existing1", "existing2"}, NoPaidBindings: true, NoRoutesOrTriggers: true, WorkersDevDisabled: true, MaxBootstrap: 1, MaxStage: 1, MaxCleanup: 1, MaxGET: 12}
	return pub, priv, e
}
func sign(priv ed25519.PrivateKey, e Evidence) SignedEvidence {
	return SignedEvidence{Evidence: e, Signature: ed25519.Sign(priv, signatureMessage(e))}
}
func TestOwnerAttestedFreePlanRequiresLiveInventory(t *testing.T) {
	pub, priv, e := gateFixture(t)
	reads := 0
	f := func(_ context.Context, _ string) ([]string, error) { reads++; return slices.Clone(e.Workers), nil }
	d, err := Admit(context.Background(), time.Now(), strings.Repeat("a", 32), pub, sign(priv, e), f)
	if err != nil || d.Status != "OWNER_ATTESTED_SCOPED" || d.AuthorizesExecution || d.Authoritative || reads != 1 {
		t.Fatalf("invalid preflight %+v %v reads=%d", d, err, reads)
	}
}
func TestMissingOrForgedFreeEvidenceBlocksWithoutProviderRead(t *testing.T) {
	pub, priv, e := gateFixture(t)
	calls := 0
	read := func(context.Context, string) ([]string, error) { calls++; return e.Workers, nil }
	for _, mode := range []string{"tampered", "missing", "expired", "cost", "paid-bindings", "wide-scope", "wrong-account", "untrusted-key", "missing-workers"} {
		t.Run(mode, func(t *testing.T) {
			x := e
			key := pub
			switch mode {
			case "tampered":
				x.WorkerName = "kc-v09-stage-pilot-other"
			case "missing":
				x.Plan = "unknown"
			case "expired":
				x.ExpiresUnix = time.Now().Add(-time.Second).Unix()
			case "cost":
				x.BudgetExtraUSDCents = 1
			case "paid-bindings":
				x.NoPaidBindings = false
			case "wide-scope":
				x.WorkerName = "context-platform-gateway-p1"
			case "wrong-account":
				x.AccountID = strings.Repeat("b", 32)
			case "untrusted-key":
				key = ed25519.PublicKey(strings.Repeat("x", ed25519.PublicKeySize))
			case "missing-workers":
				x.Workers = nil
			}
			s := sign(priv, e)
			if mode != "untrusted-key" {
				if mode != "tampered" {
					s = sign(priv, x)
				} else {
					s.Evidence = x
				}
			}
			if d, err := Admit(context.Background(), time.Now(), strings.Repeat("a", 32), key, s, read); err == nil || d.Status != "BLOCKED" {
				t.Fatalf("accepted %s", mode)
			}
		})
	}
	if calls != 0 {
		t.Fatalf("provider read made for invalid claims: %d", calls)
	}
}
func TestInventoryChangeOrCapacityBlocks(t *testing.T) {
	pub, priv, e := gateFixture(t)
	cases := []struct {
		name string
		live []string
	}{{"changed", []string{"existing1"}}, {"at-capacity", func() []string {
		xs := make([]string, 100)
		for i := range xs {
			xs[i] = fmt.Sprintf("worker-%03d", i)
		}
		return xs
	}()}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			x := e
			if tt.name == "at-capacity" {
				x.Workers = tt.live
			}
			_, err := Admit(context.Background(), time.Now(), strings.Repeat("a", 32), pub, sign(priv, x), func(context.Context, string) ([]string, error) { return tt.live, nil })
			if err == nil {
				t.Fatal("accepted unsafe capacity/inventory")
			}
		})
	}
}
func TestStageRequiresExistingDisposableTarget(t *testing.T) {
	pub, priv, e := gateFixture(t)
	e.Operation = "stage"
	if _, err := Admit(context.Background(), time.Now(), strings.Repeat("a", 32), pub, sign(priv, e), func(context.Context, string) ([]string, error) { return e.Workers, nil }); err == nil {
		t.Fatal("stage without target")
	}
	e.Workers = []string{"existing1", e.WorkerName}
	if _, err := Admit(context.Background(), time.Now(), strings.Repeat("a", 32), pub, sign(priv, e), func(context.Context, string) ([]string, error) { return e.Workers, nil }); err != nil {
		t.Fatal(err)
	}
}
