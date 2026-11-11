package providers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/delivery/cloudflaredelivery"
	"github.com/camem2442/keycli/internal/delivery/journal"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/hostbridge"
	"github.com/camem2442/keycli/internal/rotation"
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

func (o *cloudflareObserver) GetVersion(context.Context, string, string, string) (cloudflare.VersionMetadata, error) {
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
		})
	}
}
