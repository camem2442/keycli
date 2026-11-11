package providers

import (
	"context"
	"crypto/sha256"

	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
)

const (
	oracleTestRegion      = "ap-osaka-1"
	oracleTestSecret      = "ocid1.vaultsecret.oc1.ap-osaka-1.fixture"
	oracleTestVault       = "ocid1.vault.oc1.ap-osaka-1.fixture"
	oracleTestCompartment = "ocid1.compartment.oc1..fixture"
	oracleTestTenancy     = "ocid1.tenancy.oc1..fixture"
	oracleTestETag        = `"fixture-etag"`
	oracleTestPath        = "/20180608/secrets/" + oracleTestSecret
)

func oracleTestTarget() provideridentity.OracleVaultVersion {
	return provideridentity.OracleVaultVersion{
		PlanTarget: "fixture", Region: oracleTestRegion, SecretID: oracleTestSecret, VaultID: oracleTestVault,
		CompartmentID: oracleTestCompartment, TenancyID: oracleTestTenancy, Version: 7,
		VerifySignerTenancy: func(_ context.Context, tenancy string) error {
			if tenancy != oracleTestTenancy {
				return errors.New("tenancy mismatch")
			}
			return nil
		},
	}
}

func oracleTestReply(status int, body, etag string) *http.Response {
	header := make(http.Header)
	if etag != "" {
		header.Set("ETag", etag)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}
}

func oracleTestSecretReply(etag string) *http.Response {
	return oracleTestReply(http.StatusOK, fmt.Sprintf(`{"id":%q,"compartmentId":%q,"vaultId":%q,"lifecycleState":"ACTIVE"}`, oracleTestSecret, oracleTestCompartment, oracleTestVault), etag)
}

func TestOracleGatewayStagesExactPendingVersion(t *testing.T) {
	for _, tc := range []struct {
		name, result                      string
		etagConflict, lostReply, promoted bool
	}{
		{"verified pending", effectgateway.StatusApplied, false, false, false},
		{"etag conflict", effectgateway.StatusNoEffect, true, false, false},
		{"lost write reply", effectgateway.StatusUnknown, false, true, false},
		{"current moved", effectgateway.StatusUnknown, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			target := oracleTestTarget()
			gateway, req, key, plan := providerGateway(t, "oracle", target.PlanTarget, oracleStageOperation, hostbridge.ActionStage, "temporary")
			marker := OraclePendingVersionName(req.MutationID, plan.ID)
			req.TargetDigest = hostbridge.TargetDigest("oracle", oracleStageOperation, target.StageTarget(oracleTestETag, marker))
			remoteETag := oracleTestETag
			if tc.etagConflict {
				remoteETag = "changed-etag"
			}
			published, fences, writes, loads := false, 0, 0, 0
			reader, err := provideridentity.NewReader(func(*http.Request) error { return nil }, githubTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet || r.URL.Host != "vaults."+oracleTestRegion+".oci.oraclecloud.com" {
					t.Fatalf("unexpected identity request %s %s", r.Method, r.URL)
				}
				switch r.URL.Path {
				case oracleTestPath:
					return oracleTestSecretReply(remoteETag), nil
				case oracleTestPath + "/versions":
					summary := func(id int, name, stages string) string {
						return fmt.Sprintf(`{"secretId":%q,"versionNumber":%d,"name":%q,"timeCreated":"2026-01-01T00:00:00Z","stages":%s}`, oracleTestSecret, id, name, stages)
					}
					old := summary(7, "old", `["CURRENT"]`)
					if !published {
						return oracleTestReply(http.StatusOK, "["+old+"]", ""), nil
					}
					stage := `["PENDING","LATEST"]`
					if tc.promoted {
						old = summary(7, "old", `["PREVIOUS"]`)
						stage = `["CURRENT","LATEST"]`
					}
					return oracleTestReply(http.StatusOK, "["+old+","+summary(8, marker, stage)+"]", ""), nil
				default:
					t.Fatalf("unexpected identity path %s", r.URL.Path)
					return nil, nil
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			target.Reader = reader
			signed := false
			client, err := providerlifecycle.NewClient(func(r *http.Request) error {
				signed = true
				r.Header.Set("Authorization", "synthetic-fixture")
				return nil
			}, githubTransport(func(r *http.Request) (*http.Response, error) {
				writes++
				if r.Method != http.MethodPut || r.URL.Path != oracleTestPath || r.Header.Get("If-Match") != oracleTestETag || fences != 1 {
					t.Fatal("unexpected staging request or missing final fence")
				}
				var payload struct{ SecretContent struct{ Stage, Name string } }
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.SecretContent.Stage != "PENDING" || payload.SecretContent.Name != marker {
					t.Fatal("incorrect stage payload")
				}
				published = true
				if tc.lostReply {
					return nil, io.ErrUnexpectedEOF
				}
				return oracleTestSecretReply("updated"), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			gateway.Authority = githubAuthority(func() error {
				if !signed {
					t.Fatal("final fence preceded signing")
				}
				fences++
				return nil
			})
			gateway.RotationActions = OracleRotation{Plan: plan, Stage: &OracleStage{
				MutationID: req.MutationID, Target: target, ETag: oracleTestETag, Writer: client, SourceHandle: "host-source",
				Load: func(_ context.Context, handle string) ([]byte, error) {
					loads++
					record, err := gateway.Journal.Status(req.MutationID)
					if err != nil || record.State != journal.UnknownEffect || handle != "host-source" {
						t.Fatal("source loaded outside durable dispatch")
					}
					return []byte("synthetic"), nil
				},
				Authorize: func(context.Context, string, rotation.PreparationPlan) (rotation.PreparationGrant, error) {
					return rotation.PreparationGrant{PlanDigest: plan.ID, GrantDigest: req.GrantDigest, ExpiresAt: req.ExpiresAt}, nil
				},
			}}
			envelope := effectgateway.Sign(req, key)
			result, err := gateway.Submit(ctx, envelope)
			wantCalls := 1
			if tc.etagConflict {
				wantCalls = 0
			}
			if err != nil || result.Status != tc.result || writes != wantCalls || loads != wantCalls {
				t.Fatalf("receipt=%+v writes=%d loads=%d err=%v", result, writes, loads, err)
			}
			if _, err := gateway.Submit(ctx, envelope); writes != wantCalls {
				t.Fatalf("replay wrote again: %v", err)
			}
			if tc.lostReply || tc.promoted {
				binding := authoritybinding.Binding{Provider: "oracle", MutationID: req.MutationID, PlanDigest: plan.ID, TargetDigest: req.TargetDigest}
				recovery := OraclePendingStageRecovery{Binding: binding, Target: target, ETag: oracleTestETag}
				got, err := recovery.ObserveRecovery(ctx, binding)
				if tc.lostReply && (err != nil || got.State != journal.ObservedApplied) {
					t.Fatalf("could not recover: %+v %v", got, err)
				}
				if tc.promoted {
					if !errors.Is(err, journal.ErrUnknownEffect) {
						t.Fatalf("Current mistaken for Pending: %+v %v", got, err)
					}
					if _, err := target.VerifyCurrent(ctx, 8, marker); err != nil {
						t.Fatalf("could not observe exact promotion: %v", err)
					}
				}
			}
		})
	}
}

func TestOracleGatewaySchedulesOnlyPinnedOldVersion(t *testing.T) {
	for _, tc := range []struct {
		name, result       string
		changed, lostReply bool
	}{
		{"scheduled", effectgateway.StatusApplied, false, false},
		{"old etag changed", effectgateway.StatusNoEffect, true, false},
		{"schedule reply lost", effectgateway.StatusUnknown, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			target := oracleTestTarget()
			now := time.Now()
			deleteAt := now.Add(48 * time.Hour).UTC().Truncate(time.Second)
			canonical := target.DeletionTarget(oracleTestETag, deleteAt)
			gateway, req, key, plan := providerGateway(t, "oracle", target.PlanTarget, oracleDeleteOperation, hostbridge.ActionRevoke, canonical)
			scheduled := ""
			currentETag := oracleTestETag
			const oldPath = oracleTestPath + "/version/7"
			reader, err := provideridentity.NewReader(func(*http.Request) error { return nil }, githubTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet {
					t.Fatal("identity reader attempted a write")
				}
				switch r.URL.Path {
				case oracleTestPath:
					return oracleTestSecretReply("secret-etag"), nil
				case oldPath:
					deletion := "null"
					if scheduled != "" {
						deletion = fmt.Sprintf("%q", scheduled)
					}
					return oracleTestReply(http.StatusOK, fmt.Sprintf(`{"secretId":%q,"versionNumber":7,"stages":["DEPRECATED"],"timeCreated":"2026-01-01T00:00:00Z","timeOfDeletion":%s}`, oracleTestSecret, deletion), currentETag), nil
				default:
					t.Fatalf("unexpected identity path %s", r.URL.Path)
					return nil, nil
				}
			}))
			if err != nil {
				t.Fatal(err)
			}
			target.Reader = reader
			old, err := target.VerifyWithETag(ctx, plan, "7", oracleTestETag)
			if err != nil {
				t.Fatal(err)
			}
			if tc.changed {
				currentETag = "changed-etag"
			}
			signed, writes, fences := false, 0, 0
			revoker, err := providerlifecycle.NewClient(func(r *http.Request) error {
				signed = true
				r.Header.Set("Authorization", "synthetic-fixture")
				return nil
			}, githubTransport(func(r *http.Request) (*http.Response, error) {
				writes++
				if r.Method != http.MethodPost || r.URL.Path != oldPath+"/actions/scheduleDeletion" || r.Header.Get("If-Match") != oracleTestETag || fences != 1 {
					t.Fatal("unexpected old version request or missing fence")
				}
				var payload struct{ TimeOfDeletion string }
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || payload.TimeOfDeletion != deleteAt.Format(time.RFC3339) {
					t.Fatal("unexpected deletion schedule")
				}
				scheduled, currentETag = payload.TimeOfDeletion, "post-schedule-etag"
				if tc.lostReply {
					return nil, io.ErrUnexpectedEOF
				}
				return oracleTestReply(http.StatusNoContent, "", ""), nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			gateway.Authority = githubAuthority(func() error {
				if !signed {
					t.Fatal("revocation fence preceded signing")
				}
				fences++
				return nil
			})
			activation := rotation.ActivationEvidence{StageReceiptDigest: strings.Repeat("b", 64), PromotionReceiptDigest: strings.Repeat("c", 64), ActivatedAt: now.Add(-time.Hour), GraceUntil: now.Add(-time.Minute)}
			consumers := []rotation.ConsumerEvidence{{ConsumerID: "runner", Generation: 2, ObservedAt: now, EvidenceDigest: strings.Repeat("d", 64)}}
			raw, err := json.Marshal(struct {
				Activation rotation.ActivationEvidence
				Consumers  []rotation.ConsumerEvidence
			}{activation, consumers})
			if err != nil {
				t.Fatal(err)
			}
			gateway.RotationActions = OracleRotation{Plan: plan, Retirement: &OracleRetirement{
				MutationID: req.MutationID, Target: target, ETag: oracleTestETag, DeleteAt: deleteAt, Revoker: revoker, Activation: activation, Consumers: consumers,
				VerifyActivation: func(context.Context, rotation.PreparationPlan, rotation.ActivationEvidence) error { return nil },
				VerifyConsumer:   func(context.Context, rotation.PreparationPlan, rotation.ConsumerEvidence) error { return nil },
				Authorize: func(context.Context, string, rotation.PreparationPlan, rotation.RetirementDecision) (rotation.RevocationGrant, error) {
					return rotation.RevocationGrant{PlanDigest: plan.ID, GrantDigest: req.GrantDigest, OldCredentialHandle: "7", OldIdentityDigest: rotation.RevocationIdentityDigest(old), EvidenceDigest: fmt.Sprintf("%x", sha256.Sum256(raw)), ExpiresAt: req.ExpiresAt}, nil
				},
			}}
			envelope := effectgateway.Sign(req, key)
			result, err := gateway.Submit(ctx, envelope)
			wantWrites := 1
			if tc.changed {
				wantWrites = 0
			}
			if err != nil || result.Status != tc.result || writes != wantWrites {
				t.Fatalf("receipt=%+v writes=%d err=%v", result, writes, err)
			}
			if tc.result == effectgateway.StatusApplied && !strings.Contains(result.Note, "permanent deletion requires separate observation") {
				t.Fatalf("receipt conflated schedule and deletion: %+v", result)
			}
			if _, err := gateway.Submit(ctx, envelope); writes != wantWrites {
				t.Fatalf("replay wrote again: %v", err)
			}
			if tc.lostReply {
				binding := authoritybinding.Binding{Provider: "oracle", MutationID: req.MutationID, PlanDigest: plan.ID, TargetDigest: req.TargetDigest}
				recovery := effectgateway.OracleDeletionScheduleRecovery{Binding: binding, Plan: plan, Version: target, DeleteAt: deleteAt, ETag: oracleTestETag}
				got, err := recovery.ObserveRecovery(ctx, binding)
				if err != nil || got.State != journal.ObservedApplied || got.ProviderIdentityDigest != rotation.RevocationIdentityDigest(old) {
					t.Fatalf("did not corroborate exact schedule: %+v %v", got, err)
				}
				scheduled = deleteAt.Add(time.Minute).Format(time.RFC3339)
				if _, err := recovery.ObserveRecovery(ctx, binding); !errors.Is(err, journal.ErrUnknownEffect) {
					t.Fatalf("accepted changed schedule: %v", err)
				}
			}
		})
	}
}
