package providers

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
)

func TestRetirementEvidenceRequiresEnrolledSignerAndExactOperation(t *testing.T) {
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	registration := effectgateway.RegisteredOperation{
		Identity: hostbridge.Identity{OperationID: "rotation-retirement", MutationID: "revoke-old"},
		Provider: "cloudflare", ProviderOperation: cloudflareDeleteOperation,
		CanonicalTarget: strings.Repeat("a", 32) + "/tokens/" + strings.Repeat("b", 32),
		PlanDigest:      strings.Repeat("c", 64),
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "evidence.json")
	for _, tc := range []struct {
		name, operation, mutation string
		key                       ed25519.PublicKey
		want                      bool
	}{
		{"exact operation and mutation", "rotation-retirement", "revoke-old", public, true},
		{"unregistered operation", "rotation-other", "revoke-old", public, false},
		{"unregistered mutation", "rotation-retirement", "revoke-other", public, false},
		{"unenrolled signer", "rotation-retirement", "revoke-old", other, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{
				"operation_id": tc.operation, "mutation_id": tc.mutation, "plan_digest": registration.PlanDigest,
				"target_digest": registration.Binding().TargetDigest,
				"activation":    rotation.ActivationEvidence{}, "consumers": []rotation.ConsumerEvidence{},
			})
			if err != nil {
				t.Fatal(err)
			}
			signature := ed25519.Sign(private, append([]byte(retirementEvidenceDomain), payload...))
			envelope, err := json.Marshal(map[string][]byte{"payload": payload, "signature": signature})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, envelope, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = readRetirementEvidence(path, tc.key, registration)
			if (err == nil) != tc.want {
				t.Fatalf("unexpected evidence result: %v", err)
			}
		})
	}
}

func TestLoadRetirementRegistersRecoveryForExactOperation(t *testing.T) {
	account, token := strings.Repeat("a", 32), strings.Repeat("b", 32)
	old := rotation.Generation{Alias: "fixture", Kind: rotation.StaticServiceKey, Provider: "cloudflare", Target: account, Number: 1}
	next := old
	next.Number++
	plan, err := rotation.NewPreparationPlan(old, next, []rotation.Consumer{{ID: "runner", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	registration := effectgateway.RegisteredOperation{
		Identity: hostbridge.Identity{GoalID: "goal", OperationID: "retire", MutationID: "retire-old",
			WriterGeneration: 1, WriterOwner: "writer", WriterSession: "session"},
		Provider: "cloudflare", ProviderOperation: cloudflareDeleteOperation,
		CanonicalTarget: account + "/tokens/" + token, ResourceDigest: strings.Repeat("c", 64),
		PlanDigest: plan.ID, GrantDigest: strings.Repeat("d", 64),
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	accountFile, revokerFile := filepath.Join(dir, "account"), filepath.Join(dir, "revoker")
	for path, value := range map[string]string{accountFile: "fixture-account", revokerFile: "fixture-revoker"} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	policies := fmt.Sprintf(`[{"effect":"allow","resources":{"com.cloudflare.api.account.%s":"*"}}]`, account)
	var policy any
	if err := json.Unmarshal([]byte(policies), &policy); err != nil {
		t.Fatal(err)
	}
	canonicalPolicy, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	policyDigest := fmt.Sprintf("%x", sha256.Sum256(canonicalPolicy))
	issued := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	raw, err := json.Marshal(map[string]any{
		"goal_id": "goal", "mutation_id": "retire-old", "account_id": account, "credential_alias": old.Alias,
		"old_generation": old.Number, "new_generation": next.Number, "required_consumers": []string{"runner"},
		"old_token_id": token, "old_token_name": "kc-rot-old", "old_token_issued_on": issued,
		"old_policy_digest": policyDigest, "old_identity_digest": strings.Repeat("e", 64),
		"account_reader_file": accountFile, "revoker_file": revokerFile,
		"evidence_file": filepath.Join(dir, "evidence"), "evidence_public_key": make([]byte, ed25519.PublicKeySize),
		"minimum_grace_seconds": 1, "grant_expires_at": time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Fixture readers supply their own credentials and never open a network socket.
	fixtureAuth := func(token string) provideridentity.Authenticate {
		return func(req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}
	}
	tokenStatus := http.StatusOK
	tokenReads := 0
	tokenReader, err := provideridentity.NewReader(fixtureAuth("fixture-reader"), githubTransport(func(req *http.Request) (*http.Response, error) {
		tokenReads++
		if req.Method != http.MethodGet || req.URL.Path != "/client/v4/accounts/"+account+"/tokens/"+token ||
			req.Header.Get("Authorization") != "Bearer fixture-reader" {
			t.Fatal("unexpected token observation")
		}
		return &http.Response{StatusCode: tokenStatus, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(
			`{"success":true,"result":{"id":%q,"name":"kc-rot-old","status":"active","issued_on":%q,"policies":%s}}`,
			token, issued, policies)))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	op, err := loadRetirement(raw, registration, tokenReader, "fixture-issuer", "fixture-reader")
	if err != nil {
		t.Fatal(err)
	}
	recovery, ok := op.recovery.(effectgateway.CloudflareRevocationRecovery)
	if !ok || recovery.Binding != registration.Binding() || recovery.Plan.ID != plan.ID ||
		recovery.CanonicalTarget != registration.CanonicalTarget || recovery.Token.AccountReader == nil ||
		recovery.Token.TokenID != token || recovery.Token.ExpectedPolicyDigest != policyDigest {
		t.Fatalf("recovery missing or bound to the wrong operation")
	}
	accountReader, err := provideridentity.NewReader(fixtureAuth("fixture-account"), githubTransport(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/client/v4/accounts/"+account ||
			req.Header.Get("Authorization") != "Bearer fixture-account" {
			t.Fatal("unexpected account observation")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(
			`{"success":true,"result":{"id":%q}}`, account)))}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	recovery.Token.AccountReader = &accountReader
	evidence, err := recovery.ObserveRecovery(context.Background(), registration.Binding())
	if err != nil || evidence.State != journal.ObservedNoEffect {
		t.Fatalf("active token: state=%v err=%v", evidence.State, err)
	}
	tokenStatus = http.StatusNotFound
	if _, err := recovery.ObserveRecovery(context.Background(), registration.Binding()); !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatalf("missing token: want unknown, got %v", err)
	}
	wrong := registration.Binding()
	wrong.MutationID = "another-mutation"
	if _, err := recovery.ObserveRecovery(context.Background(), wrong); !errors.Is(err, journal.ErrUnknownEffect) || tokenReads != 2 {
		t.Fatalf("wrong binding: err=%v token reads=%d", err, tokenReads)
	}
}
