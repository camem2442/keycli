package providers

import (
	"context"
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

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/cloudflaredelivery"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
	"github.com/camem2442/keycli/internal/target/cloudflare"
)

type cloudflareObserver struct {
	before, after cloudflare.DeploymentSnapshot
	tag           string
	reads         int
}

func (o *cloudflareObserver) Observe(context.Context, string, string) (cloudflare.DeploymentSnapshot, error) {
	o.reads++
	if o.reads == 1 {
		return o.before, nil
	}
	return o.after, nil
}

func (o *cloudflareObserver) GetVersion(_ context.Context, _, _, id string) (cloudflare.VersionMetadata, error) {
	if id == "old" {
		return cloudflare.VersionMetadata{ID: id}, nil
	}
	return cloudflare.VersionMetadata{ID: "new", Tag: o.tag, BindingNames: []string{"FIXTURE"}}, nil
}

func TestCloudflareStageGatewayPreservesActiveTraffic(t *testing.T) {
	for _, tc := range []struct {
		name, childExit, status string
		drift                   bool
	}{
		{"verified", "0", effectgateway.StatusApplied, false},
		{"traffic drift", "0", effectgateway.StatusUnknown, true},
		{"child failed after start", "1", effectgateway.StatusUnknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			account := strings.Repeat("a", 32)
			gateway, req, key, plan := providerGateway(t, "cloudflare", "fixture", cloudflareStageOperation, hostbridge.ActionStage, account+"/worker/prod/FIXTURE")
			dir := t.TempDir()
			exe, config := filepath.Join(dir, "wrangler"), filepath.Join(dir, "wrangler.toml")
			script := []byte("#!/bin/sh\nexit " + tc.childExit + "\n")
			if err := os.WriteFile(exe, script, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(config, nil, 0600); err != nil {
				t.Fatal(err)
			}
			observer := &cloudflareObserver{
				before: cloudflare.DeploymentSnapshot{AccountID: account, Worker: "worker", DeploymentID: "live", Versions: []cloudflare.VersionPercentage{{VersionID: "old", Percentage: 100}}, LatestVersionIDs: []string{"old"}},
				tag:    "kc-" + req.MutationID,
			}
			observer.after = observer.before
			observer.after.LatestVersionIDs = []string{"new", "old"}
			if tc.drift {
				observer.after.Versions = []cloudflare.VersionPercentage{{VersionID: "old", Percentage: 99}, {VersionID: "new", Percentage: 1}}
			}
			loads := 0
			stage := &CloudflareWorkerStage{
				MutationID:     req.MutationID,
				Request:        cloudflare.VersionStageRequest{WranglerPath: exe, ConfigPath: config, WorkDirectory: dir, AccountID: account, Worker: "worker", Environment: "prod", SecretName: "FIXTURE", Env: []string{"CLOUDFLARE_ACCOUNT_ID=" + account, "CLOUDFLARE_API_TOKEN=synthetic"}},
				WranglerSHA256: fmt.Sprintf("%x", sha256.Sum256(script)), SourceHandle: "host-source", Observer: observer,
				ExpectedActiveDigest: cloudflaredelivery.ActiveDigest(observer.before),
				Authorize: func(context.Context, string, rotation.PreparationPlan) (rotation.PreparationGrant, error) {
					return rotation.PreparationGrant{PlanDigest: plan.ID, GrantDigest: req.GrantDigest, ExpiresAt: time.Now().Add(time.Minute)}, nil
				},
				Load: func(_ context.Context, handle string) ([]byte, error) {
					loads++
					if record, err := gateway.Journal.Status(req.MutationID); handle != "host-source" || err != nil || record.State != journal.UnknownEffect {
						t.Fatal("source load escaped the original journal")
					}
					return []byte("fixture"), nil
				},
			}
			gateway.RotationActions = CloudflareRotation{Plan: plan, AccountID: account, WorkerStage: stage}
			envelope := effectgateway.Sign(req, key)
			result, err := gateway.Submit(ctx, envelope)
			if result.Status != tc.status || loads != 1 || observer.reads != 2 {
				t.Fatalf("status=%s loads=%d reads=%d err=%v", result.Status, loads, observer.reads, err)
			}
			if _, err := gateway.Submit(ctx, envelope); loads != 1 {
				t.Fatalf("stage replay loaded the source again: %v", err)
			}
			if tc.status == effectgateway.StatusUnknown {
				binding := authoritybinding.Binding{Provider: "cloudflare", MutationID: req.MutationID, TargetDigest: req.TargetDigest}
				recovery := CloudflareWorkerStageRecovery{Binding: binding, AccountID: account, Worker: "worker", Environment: "prod", Destination: "FIXTURE", ExpectedActiveDigest: stage.ExpectedActiveDigest, Observer: observer}
				evidence, err := recovery.ObserveRecovery(ctx, binding)
				if tc.drift {
					if !errors.Is(err, journal.ErrUnknownEffect) {
						t.Fatalf("accepted traffic drift on recovery: %+v %v", evidence, err)
					}
				} else if err != nil || evidence.State != journal.ObservedApplied {
					t.Fatalf("could not verify exact inactive version after child failure: %+v %v", evidence, err)
				}
			}

		})
	}
}

type cloudflareRecoverySink map[string]string

func (s cloudflareRecoverySink) Store(_ context.Context, operationID, tokenID string, _ []byte) error {
	s[operationID] = tokenID
	return errors.New("store reply lost")
}

func (s cloudflareRecoverySink) Lookup(_ context.Context, operationID string) (string, bool, error) {
	token, found := s[operationID]
	return token, found, nil
}

func TestCloudflareIssueLostStoreReplyRecoversExactToken(t *testing.T) {
	ctx := context.Background()
	account, group, token := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
	name := "kc-rot-fixture"
	canonical := account + "/tokens/" + name + "/permission-group/" + group
	gateway, req, key, plan := providerGateway(t, "cloudflare", "fixture", cloudflareIssueOperation, hostbridge.ActionIssue, canonical)
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	sink := cloudflareRecoverySink{}
	reply := func(body string) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
	}
	posts := 0
	issuer, err := providerlifecycle.NewClient(func(*http.Request) error { return nil }, githubTransport(func(r *http.Request) (*http.Response, error) {
		posts++
		if r.Method != http.MethodPost || r.URL.Path != "/client/v4/accounts/"+account+"/tokens" {
			t.Fatal("issuance escaped its account")
		}
		return reply(fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":%q,"value":"synthetic"}}`, token, name)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	issuerPermissions, err := provideridentity.NewReader(func(*http.Request) error { return nil }, githubTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/client/v4/accounts/"+account+"/tokens/permission_groups" {
			t.Fatalf("issuer preflight escaped the permission group endpoint: %s %s", r.Method, r.URL.Path)
		}
		return reply(fmt.Sprintf(`{"success":true,"result":[{"id":%q,"name":"Workers Scripts Read","scopes":["com.cloudflare.api.account"],"is_selectable":true}]}`, group)), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	reportedToken := token
	reader, err := provideridentity.NewReader(func(*http.Request) error { return nil }, githubTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet {
			t.Fatal("recovery attempted a write")
		}
		switch r.URL.Path {
		case "/client/v4/accounts/" + account + "/tokens/" + token:
			return reply(fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":%q,"status":"active","expires_on":%q,"policies":[{"effect":"allow","resources":{"com.cloudflare.api.account.%s":"*"},"permission_groups":[{"id":%q}]}]}}`, reportedToken, name, expiry.Format(time.RFC3339), account, group)), nil
		default:
			t.Fatalf("unexpected read %s", r.URL.Path)
			return nil, nil
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	issue := CloudflareTokenIssue{
		MutationID: req.MutationID, Name: name, PermissionGroupID: group, ExpiresAt: expiry, Issuer: issuer, IssuerPermissions: issuerPermissions, PermissionReader: reader, Sink: sink,
		Authorize: func(context.Context, string, rotation.PreparationPlan) (rotation.PreparationGrant, error) {
			return rotation.PreparationGrant{PlanDigest: plan.ID, GrantDigest: req.GrantDigest, ExpiresAt: req.ExpiresAt}, nil
		},
	}
	gateway.RotationActions = CloudflareRotation{Plan: plan, AccountID: account, RuntimePermissionGroups: []string{group}, Issue: &issue}
	envelope := effectgateway.Sign(req, key)
	if result, err := gateway.Submit(ctx, envelope); result.Status != effectgateway.StatusUnknown || posts != 1 {
		t.Fatalf("unexpected issuance status %s posts=%d err=%v", result.Status, posts, err)
	}
	if _, err := gateway.Submit(ctx, envelope); posts != 1 {
		t.Fatalf("replay issued another token: %v", err)
	}
	binding := authoritybinding.Binding{Provider: "cloudflare", MutationID: req.MutationID, TargetDigest: req.TargetDigest}
	recovery := CloudflareIssueRecovery{Binding: binding, AccountID: account, Issue: issue}
	if got, err := recovery.ObserveRecovery(ctx, binding); err != nil || got.State != journal.ObservedApplied || got.ProviderIdentityDigest != recovery.IdentityDigest() {
		t.Fatalf("could not recover committed store record: %+v %v", got, err)
	}
	reportedToken = strings.Repeat("d", 32)
	if _, err := recovery.ObserveRecovery(ctx, binding); !errors.Is(err, journal.ErrUnknownEffect) {
		t.Fatalf("accepted changed token ID: %v", err)
	}
}

func TestCloudflareRetireGatewayRequiresExactOldToken(t *testing.T) {
	for _, tc := range []struct {
		name, status       string
		wrongID, loseReply bool
	}{
		{"acknowledged", effectgateway.StatusApplied, false, false},
		{"different generation", effectgateway.StatusNoEffect, true, false},
		{"lost delete reply", effectgateway.StatusUnknown, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			account, old, replacement := strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)
			canonical := account + "/tokens/" + old
			gateway, req, key, plan := providerGateway(t, "cloudflare", "fixture", cloudflareDeleteOperation, hostbridge.ActionRevoke, canonical)
			const issued = "2026-01-01T00:00:00Z"
			policy := fmt.Sprintf(`[{"effect":"allow","resources":{"com.cloudflare.api.account.%s":"*"}}]`, account)
			var canonicalPolicy any
			if err := json.Unmarshal([]byte(policy), &canonicalPolicy); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(canonicalPolicy)
			if err != nil {
				t.Fatal(err)
			}
			policyDigest := fmt.Sprintf("%x", sha256.Sum256(data))
			accountPath := "/client/v4/accounts/" + account
			tokenPath := accountPath + "/tokens/" + old
			readID := old
			reader, err := provideridentity.NewReader(func(*http.Request) error { return nil }, githubTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatal("identity reader attempted a mutation")
				}
				switch r.URL.Path {
				case accountPath:
					return githubResponse(http.StatusOK, fmt.Sprintf(`{"success":true,"result":{"id":%q}}`, account)), nil
				case tokenPath:
					return githubResponse(http.StatusOK, fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":"kc-rot-old","status":"active","issued_on":%q,"policies":%s}}`, readID, issued, policy)), nil
				default:
					t.Fatalf("unexpected identity path %s", r.URL.Path)
					return nil, nil
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			target := provideridentity.CloudflareAccountToken{Reader: reader, PlanTarget: plan.Old.Target, AccountID: account, TokenID: old, ExpectedName: "kc-rot-old", ExpectedIssuedOn: issued, ExpectedPolicyDigest: policyDigest}
			verified, err := target.Verify(ctx, plan, old)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wrongID {
				readID = replacement
			}
			deletes := 0
			client, err := providerlifecycle.NewClient(func(*http.Request) error { return nil }, githubTransport(func(r *http.Request) (*http.Response, error) {
				deletes++
				if r.Method != http.MethodDelete || r.URL.Path != tokenPath {
					t.Fatalf("revoker targeted %s %s", r.Method, r.URL.Path)
				}
				if tc.loseReply {
					return nil, io.ErrUnexpectedEOF
				}
				return githubResponse(http.StatusOK, fmt.Sprintf(`{"success":true,"result":{"id":%q}}`, old)), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			activation := rotation.ActivationEvidence{StageReceiptDigest: strings.Repeat("b", 64), PromotionReceiptDigest: strings.Repeat("c", 64), ActivatedAt: now.Add(-time.Hour), GraceUntil: now.Add(-time.Minute)}
			consumers := []rotation.ConsumerEvidence{{ConsumerID: "runner", Generation: 2, ObservedAt: now, EvidenceDigest: strings.Repeat("d", 64)}}
			evidence, err := json.Marshal(struct {
				Activation rotation.ActivationEvidence
				Consumers  []rotation.ConsumerEvidence
			}{activation, consumers})
			if err != nil {
				t.Fatal(err)
			}
			gateway.RotationActions = CloudflareRotation{Plan: plan, AccountID: account, Retirement: &CloudflareRetirement{
				MutationID: req.MutationID, Target: target, Revoker: client, Activation: activation, Consumers: consumers,
				VerifyActivation: func(context.Context, rotation.PreparationPlan, rotation.ActivationEvidence) error { return nil },
				VerifyConsumer:   func(context.Context, rotation.PreparationPlan, rotation.ConsumerEvidence) error { return nil },
				Authorize: func(context.Context, string, rotation.PreparationPlan, rotation.RetirementDecision) (rotation.RevocationGrant, error) {
					return rotation.RevocationGrant{PlanDigest: plan.ID, GrantDigest: req.GrantDigest, OldCredentialHandle: old, OldIdentityDigest: rotation.RevocationIdentityDigest(verified), EvidenceDigest: fmt.Sprintf("%x", sha256.Sum256(evidence)), ExpiresAt: req.ExpiresAt}, nil
				},
			}}
			envelope := effectgateway.Sign(req, key)
			result, err := gateway.Submit(ctx, envelope)
			wantDeletes := 1
			if tc.wrongID {
				wantDeletes = 0
			}
			if result.Status != tc.status || deletes != wantDeletes {
				t.Fatalf("status=%s deletes=%d error=%v", result.Status, deletes, err)
			}
			if _, err := gateway.Submit(ctx, envelope); deletes != wantDeletes {
				t.Fatalf("replay deleted again: %v", err)
			}
		})
	}
}
