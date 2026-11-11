package effectgateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
)

func TestOracleRecoveryVerifiesExactDeletionSchedule(t *testing.T) {
	ctx := context.Background()
	const region, secret, vault, compartment, tenancy = "us-ashburn-1", "ocid1.vaultsecret.fixture", "ocid1.vault.fixture", "ocid1.compartment.fixture", "ocid1.tenancy.fixture"
	old := rotation.Generation{Alias: "fixture", Kind: rotation.StaticServiceKey, Provider: "oracle", Target: "fixture-vault", Number: 1}
	next := old
	next.Number++
	plan, err := rotation.NewPreparationPlan(old, next, []rotation.Consumer{{ID: "runner", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	created := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	deleteAt := time.Now().UTC().Truncate(time.Second).Add(48 * time.Hour)
	var observedDeletion string
	gets := 0
	reader, err := provideridentity.NewReader(func(req *http.Request) error {
		req.Header.Set("Authorization", "fixture-oci-read")
		return nil
	}, recoveryTransport(func(req *http.Request) (*http.Response, error) {
		gets++
		if req.Method != http.MethodGet || req.URL.Host != "vaults."+region+".oci.oraclecloud.com" ||
			req.Header.Get("Authorization") != "fixture-oci-read" {
			t.Fatalf("unexpected provider request: %s %s", req.Method, req.URL)
		}
		var body string
		switch req.URL.Path {
		case "/20180608/secrets/" + secret:
			body = fmt.Sprintf(`{"id":%q,"compartmentId":%q,"vaultId":%q,"lifecycleState":"ACTIVE"}`, secret, compartment, vault)
		case "/20180608/secrets/" + secret + "/version/1":
			deletion := ""
			if observedDeletion != "" {
				deletion = fmt.Sprintf(`,"timeOfDeletion":%q`, observedDeletion)
			}
			body = fmt.Sprintf(`{"secretId":%q,"versionNumber":1,"stages":["DEPRECATED"],"timeCreated":%q%s}`, secret, created, deletion)
		default:
			t.Fatalf("unexpected provider path: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	observer := provideridentity.OracleVaultVersion{
		Reader: reader, PlanTarget: old.Target, TenancyID: tenancy, CompartmentID: compartment,
		VaultID: vault, SecretID: secret, Region: region, Version: 1,
		VerifySignerTenancy: func(_ context.Context, actual string) error {
			if actual != tenancy {
				t.Fatalf("unexpected tenancy: %s", actual)
			}
			return nil
		},
	}
	original, err := observer.Verify(ctx, plan, "1")
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.gw.Now = nil
	binding := authoritybinding.Binding{
		GoalID: testGoal, MutationID: "schedule-original", WriterGeneration: 1, Owner: testOwner, Session: testSess,
		Provider: "oracle", Operation: "retire", ResourceDigest: strings.Repeat("e", 64), PlanDigest: plan.ID,
		TargetDigest: hostbridge.TargetDigest("oracle", "schedule-deprecated-vault-version-deletion", region+"/"+secret+"/version/1"),
		GrantDigest:  testGrant,
	}
	intent := journal.Intent{OperationID: binding.MutationID, TargetDigest: binding.TargetDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest}
	if _, err := h.gw.Journal.DispatchBound(intent, binding, func(journal.DispatchGuard) (journal.Observation, error) {
		return journal.Observation{}, errors.New("lost scheduling response")
	}); !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatal(err)
	}
	h.gw.RecoveryAuthority = recoveryAuthorizerFunc(func(context.Context, authoritybinding.Binding) (RecoveryGrant, error) {
		return RecoveryGrant{Binding: binding, GrantDigest: strings.Repeat("9", 64),
			ProviderIdentityDigest: rotation.RevocationIdentityDigest(original), ExpiresAt: time.Now().Add(time.Minute)}, nil
	})
	h.gw.RecoveryObserver = OracleDeletionScheduleRecovery{Binding: binding, Plan: plan, Version: observer, DeleteAt: deleteAt}
	observedDeletion = deleteAt.Add(time.Minute).Format(time.RFC3339)
	if record, err := h.gw.Reconcile(ctx, intent.OperationID); !errors.Is(err, journal.ErrUnknownEffect) || record.State != journal.UnknownEffect {
		t.Fatalf("different deletion time settled the mutation: %+v, %v", record, err)
	}
	observedDeletion = deleteAt.Format(time.RFC3339)
	if record, err := h.gw.Reconcile(ctx, intent.OperationID); err != nil || record.State != journal.ObservedApplied || !journal.ValidDigest(record.ProofDigest) {
		t.Fatalf("exact deletion schedule did not settle: %+v, %v", record, err)
	}
	if gets != 6 {
		t.Fatalf("expected original reads and two independent observations, got %d GETs", gets)
	}
}
