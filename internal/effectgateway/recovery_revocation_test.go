package effectgateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

type recoveryTransport func(*http.Request) (*http.Response, error)

func (f recoveryTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRecoveryCloudflareGETCorroboratesActiveOriginal(t *testing.T) {
	ctx := context.Background()
	account, token := strings.Repeat("a", 32), strings.Repeat("b", 32)
	const target = "fixture-account"
	old := rotation.Generation{Alias: "fixture", Kind: rotation.StaticServiceKey, Provider: "cloudflare", Target: target, Number: 1}
	next := old
	next.Number++
	plan, err := rotation.NewPreparationPlan(old, next, []rotation.Consumer{{ID: "runner", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	policies := fmt.Sprintf(`[{"effect":"allow","resources":{"com.cloudflare.api.account.%s":"*"}}]`, account)
	policyDigest := sha256.Sum256([]byte(policies))
	issued := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	var gets int
	missing := false
	reader, err := provideridentity.NewReader(func(req *http.Request) error {
		req.Header.Set("Authorization", "Bearer fixture-read-only")
		return nil
	}, recoveryTransport(func(req *http.Request) (*http.Response, error) {
		gets++
		if req.Method != http.MethodGet || req.URL.Host != "api.cloudflare.com" ||
			req.Header.Get("Authorization") != "Bearer fixture-read-only" {
			t.Fatalf("unexpected provider read: %s %s", req.Method, req.URL)
		}
		var body string
		status := http.StatusOK
		switch req.URL.Path {
		case "/client/v4/accounts/" + account:
			body = fmt.Sprintf(`{"success":true,"result":{"id":%q}}`, account)
		case "/client/v4/accounts/" + account + "/tokens/" + token:
			body = fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":"fixture","status":"active","issued_on":%q,"policies":%s}}`, token, issued, policies)
			if missing {
				status, body = http.StatusNotFound, `{"success":false}`
			}
		default:
			t.Fatalf("unexpected provider GET path: %s", req.URL.Path)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	observer := provideridentity.CloudflareAccountToken{
		Reader: reader, PlanTarget: target, AccountID: account, TokenID: token, ExpectedName: "fixture",
		ExpectedIssuedOn: issued, ExpectedPolicyDigest: hex.EncodeToString(policyDigest[:]),
	}
	identity, err := observer.Verify(ctx, plan, token)
	if err != nil {
		t.Fatal(err)
	}
	gets = 0
	h := newHarness(t)
	h.gw.Now = nil
	binding := authoritybinding.Binding{
		GoalID: testGoal, MutationID: "retire-active", WriterGeneration: 1, Owner: testOwner, Session: testSess,
		Provider: "cloudflare", Operation: "retire", ResourceDigest: strings.Repeat("e", 64), PlanDigest: plan.ID,
		TargetDigest: hostbridge.TargetDigest("cloudflare", "delete-exact-account-token", target), GrantDigest: testGrant,
	}
	for _, tc := range []struct {
		id   string
		want journal.State
	}{{"retire-active", journal.ObservedNoEffect}, {"retire-unconfirmed", journal.UnknownEffect}} {
		binding.MutationID = tc.id
		missing = tc.want == journal.UnknownEffect
		h.gw.RecoveryAuthority = recoveryAuthorizerFunc(func(context.Context, authoritybinding.Binding) (RecoveryGrant, error) {
			return RecoveryGrant{Binding: binding, GrantDigest: strings.Repeat("9", 64),
				ProviderIdentityDigest: rotation.RevocationIdentityDigest(identity), ExpiresAt: time.Now().Add(time.Minute)}, nil
		})
		h.gw.RecoveryObserver = CloudflareRevocationRecovery{Binding: binding, Plan: plan, CanonicalTarget: target, Token: observer}
		intent := journal.Intent{OperationID: tc.id, TargetDigest: binding.TargetDigest, PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest}
		if _, err := h.gw.Journal.DispatchBound(intent, binding, func(journal.DispatchGuard) (journal.Observation, error) {
			return journal.Observation{}, errors.New("lost provider reply")
		}); !errors.Is(err, journal.ErrUnknownEffect) {
			t.Fatal(err)
		}
		record, err := h.gw.Reconcile(ctx, tc.id)
		if record.State != tc.want || (err != nil) != missing {
			t.Fatalf("recovery=%+v error=%v", record, err)
		}
	}
	if gets != 4 {
		t.Fatalf("expected independent account and token reads for both operations, got %d GETs", gets)
	}
}
