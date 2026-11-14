package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
	"github.com/camem2442/keycli/internal/target/gh"
)

type githubClaims struct{ claim effectgateway.ClaimSnapshot }

func (c githubClaims) Snapshot(context.Context, string) (effectgateway.ClaimSnapshot, error) {
	return c.claim, nil
}

type githubAuthority func() error

func (a githubAuthority) Bind(context.Context, effectgateway.Request) (effectgateway.BoundAuthority, error) {
	return a, nil
}
func (a githubAuthority) Preflight(context.Context) error       { return nil }
func (a githubAuthority) VerifyExecution(context.Context) error { return a() }

type githubWriter func(context.Context, gh.SecretRESTTarget, func(context.Context) ([]byte, error), func(context.Context) error) (gh.SecretWriteOutcome, error)

func (w githubWriter) CreateRepositoryGenerationFrom(ctx context.Context, target gh.SecretRESTTarget, load func(context.Context) ([]byte, error), fence func(context.Context) error) (gh.SecretWriteOutcome, error) {
	return w(ctx, target, load, fence)
}

func githubGateway(t *testing.T, operation, action, canonical string) (*effectgateway.Gateway, effectgateway.Request, []byte, rotation.PreparationPlan) {
	t.Helper()
	return providerGateway(t, "github", "example/fixture", operation, action, canonical)
}

func providerGateway(t *testing.T, provider, planTarget, operation, action, canonical string) (*effectgateway.Gateway, effectgateway.Request, []byte, rotation.PreparationPlan) {
	t.Helper()
	now := time.Now()
	old := rotation.Generation{Alias: "fixture", Kind: rotation.StaticServiceKey, Provider: provider, Target: planTarget, Number: 1}
	next := old
	next.Number++
	plan, err := rotation.NewPreparationPlan(old, next, []rotation.Consumer{{ID: "runner", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	req := effectgateway.Request{
		GoalID: "goal", MutationID: provider + "-test", Provider: provider, Action: action,
		TargetDigest: hostbridge.TargetDigest(provider, operation, canonical),
		PlanDigest:   plan.ID, GrantDigest: strings.Repeat("a", 64), Generation: 1,
		Session: "session", Owner: "owner", ExpiresAt: now.Add(time.Minute),
	}
	keys := effectgateway.NewMemoryKeyRing(time.Minute, time.Now)
	key, err := keys.Issue(req.GoalID, req.Owner, req.Session, req.Generation)
	if err != nil {
		t.Fatal(err)
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0700); err != nil {
		t.Fatal(err)
	}
	gateway := &effectgateway.Gateway{
		Claims: githubClaims{effectgateway.ClaimSnapshot{GoalID: req.GoalID, Generation: req.Generation,
			Session: req.Session, Owner: req.Owner, State: "ACTIVE", LeaseExpiresAt: req.ExpiresAt}},
		Keys: keys, Journal: journal.Store{Root: filepath.Join(parent, "journal")},
		Authority: githubAuthority(func() error { return nil }),
	}
	return gateway, req, key, plan
}

func TestGitHubStageUsesGatewayJournalAndFinalFence(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		puts         int
	}{
		{"created", effectgateway.StatusApplied, 1},
		{"lost response", effectgateway.StatusUnknown, 1},
		{"denied after reads", effectgateway.StatusNoEffect, 0},
		{"wrong generation", effectgateway.StatusNoEffect, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			canonical, err := providerlifecycle.GitHubGenerationTarget("example/fixture", "FIXTURE_GEN_2", 11, 22, 2)
			if err != nil {
				t.Fatal(err)
			}
			gateway, req, key, plan := githubGateway(t, githubStageOperation, hostbridge.ActionStage, canonical)
			reads, fences, puts := 0, 0, 0
			gateway.Authority = githubAuthority(func() error {
				fences++
				if reads != 1 {
					t.Fatal("Gateway fence ran before provider preflight")
				}
				if tc.name == "denied after reads" {
					return hostbridge.ErrAdmissionRejected
				}
				return nil
			})
			stage := GitHubStage{
				MutationID: req.MutationID, SourceHandle: "fixture-source",
				Target: gh.SecretRESTTarget{Scope: "repository", Repository: "example/fixture", Destination: "FIXTURE_GEN_2", RepositoryID: 11, RepositoryOwnerID: 22},
				Load: func(_ context.Context, handle string) ([]byte, error) {
					if handle != "fixture-source" {
						t.Fatal("unexpected source handle")
					}
					return []byte("fixture-value"), nil
				},
				Authorize: func(context.Context, string, rotation.PreparationPlan) (rotation.PreparationGrant, error) {
					return rotation.PreparationGrant{PlanDigest: plan.ID, GrantDigest: req.GrantDigest, ExpiresAt: time.Now().Add(time.Minute)}, nil
				},
				Client: githubWriter(func(ctx context.Context, target gh.SecretRESTTarget, load func(context.Context) ([]byte, error), fence func(context.Context) error) (gh.SecretWriteOutcome, error) {
					record, err := gateway.Journal.Status(req.MutationID)
					if err != nil || record.State != journal.UnknownEffect || target.Destination != "FIXTURE_GEN_2" {
						t.Fatalf("write preparation outside Gateway journal: %+v, %v", record, err)
					}
					reads++
					value, err := load(ctx)
					defer clear(value)
					if err != nil {
						return gh.SecretNotWritten, err
					}
					if err := fence(ctx); err != nil {
						return gh.SecretNotWritten, err
					}
					puts++
					if tc.name == "lost response" {
						return gh.SecretWriteUnknown, errors.New("connection ended")
					}
					return gh.SecretWriteAcknowledged, nil
				}),
			}
			if tc.name == "wrong generation" {
				stage.Target.Destination = "FIXTURE_GEN_1"
			}
			gateway.RotationActions = GitHubRotation{Plan: plan, Stage: &stage}
			envelope := effectgateway.Sign(req, key)
			result, err := gateway.Submit(ctx, envelope)
			if tc.name == "wrong generation" {
				if !errors.Is(err, effectgateway.ErrAdmissionRejected) || result.Status != tc.status || reads != 0 || fences != 0 || puts != 0 {
					t.Fatalf("generation substitution reached the provider: result=%+v reads=%d fences=%d puts=%d error=%v", result, reads, fences, puts, err)
				}
				if _, err := gateway.Journal.Status(req.MutationID); !errors.Is(err, journal.ErrNotFound) {
					t.Fatalf("generation substitution created a dispatch: %v", err)
				}
				return
			}
			if err != nil || result.Status != tc.status || result.ProviderCalls != tc.puts || puts != tc.puts || fences != 1 {
				t.Fatalf("result=%+v puts=%d fences=%d error=%v", result, puts, fences, err)
			}
			result, err = gateway.Submit(ctx, envelope)
			if err != nil || result.Status != effectgateway.StatusDuplicate || puts != tc.puts || fences != 1 {
				t.Fatalf("replay=%+v puts=%d fences=%d error=%v", result, puts, fences, err)
			}
		})
	}
}

type githubTransport func(*http.Request) (*http.Response, error)

func (f githubTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func githubResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func TestGitHubRetirementDeletesOnlyExactRepositoryGeneration(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		state        journal.State
		deletes      int
	}{
		{"delete exact old name", effectgateway.StatusApplied, journal.Acknowledged, 1},
		{"repository renamed", effectgateway.StatusNoEffect, journal.ObservedNoEffect, 0},
		{"generation metadata changed", effectgateway.StatusNoEffect, journal.ObservedNoEffect, 0},
		{"signer refuses", effectgateway.StatusNoEffect, journal.ObservedNoEffect, 0},
		{"authority denies", effectgateway.StatusNoEffect, journal.ObservedNoEffect, 0},
		{"authority outcome unknown", effectgateway.StatusUnknown, journal.UnknownEffect, 0},
		{"delete reply lost", effectgateway.StatusUnknown, journal.UnknownEffect, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			const repo, name = "example/fixture", "FIXTURE_GEN_1"
			const stamp = "2026-01-01T00:00:00Z"
			const path = "/repos/example/fixture/actions/secrets/FIXTURE_GEN_1"
			canonical, err := providerlifecycle.GitHubGenerationTarget(repo, name, 11, 22, 1)
			if err != nil {
				t.Fatal(err)
			}
			gateway, req, key, plan := githubGateway(t, githubDeleteOperation, hostbridge.ActionRevoke, canonical)
			reads, signs, fences, deletes := 0, 0, 0, 0
			const issuerStatus = "https://issuer.invalid/credentials/fixture"
			present := map[string]bool{"https://api.github.com" + path: true, issuerStatus: true}
			pin := true
			reader, err := provideridentity.NewReader(func(*http.Request) error { return nil }, githubTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Host != "api.github.com" {
					t.Fatalf("unexpected identity read %s %s", req.Method, req.URL)
				}
				reads++
				switch req.URL.Path {
				case "/repos/example/fixture":
					if !pin && tc.name == "repository renamed" {
						return githubResponse(http.StatusOK, `{"id":11,"full_name":"example/renamed","owner":{"id":22,"login":"example"}}`), nil
					}
					return githubResponse(http.StatusOK, `{"id":11,"full_name":"example/fixture","owner":{"id":22,"login":"example"}}`), nil
				case path:
					if !pin && tc.name == "generation metadata changed" {
						return githubResponse(http.StatusOK, `{"name":"FIXTURE_GEN_1","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-02T00:00:00Z"}`), nil
					}
					return githubResponse(http.StatusOK, `{"name":"FIXTURE_GEN_1","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`), nil
				}
				t.Fatalf("unexpected identity endpoint %s", req.URL.Path)
				return nil, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			identity := provideridentity.GitHubRepositorySecret{Reader: reader, PlanTarget: repo, Repository: repo, RepositoryID: 11, OwnerID: 22,
				SecretName: name, ExpectedCreatedAt: stamp, ExpectedUpdatedAt: stamp}
			old, err := identity.Verify(ctx, plan, name)
			if err != nil {
				t.Fatal(err)
			}
			pin = false
			reads = 0
			transport := githubTransport(func(request *http.Request) (*http.Response, error) {
				if request.Method == http.MethodGet && request.URL.String() == issuerStatus && present[issuerStatus] {
					return githubResponse(http.StatusOK, `{"active":true}`), nil
				}
				record, err := gateway.Journal.Status(req.MutationID)
				if request.Method != http.MethodDelete || request.URL.Host != "api.github.com" || request.URL.Path != path ||
					err != nil || record.State != journal.UnknownEffect || reads != 2 || fences != 1 {
					t.Fatalf("unexpected deletion %s %s, record=%+v reads=%d fences=%d error=%v", request.Method, request.URL, record, reads, fences, err)
				}
				deletes++
				delete(present, request.URL.String())
				if tc.name == "delete reply lost" {
					return nil, io.ErrUnexpectedEOF
				}
				return githubResponse(http.StatusNoContent, ""), nil
			})
			client, err := providerlifecycle.NewClient(func(*http.Request) error {
				signs++
				if reads != 2 || fences != 0 || deletes != 0 {
					t.Fatal("signing must follow identity verification and precede the final fence and DELETE")
				}
				if tc.name == "signer refuses" {
					return errors.New("fixture signer refused")
				}
				return nil
			}, transport)
			if err != nil {
				t.Fatal(err)
			}
			gateway.Authority = githubAuthority(func() error {
				fences++
				if signs != 1 || deletes != 0 {
					t.Fatal("the final fence must run between signing and DELETE")
				}
				switch tc.name {
				case "authority denies":
					return hostbridge.ErrAdmissionRejected
				case "authority outcome unknown":
					return hostbridge.ErrGuardedExecutionUnknown
				}
				return nil
			})
			now := time.Now()
			activation := rotation.ActivationEvidence{StageReceiptDigest: strings.Repeat("b", 64), PromotionReceiptDigest: strings.Repeat("c", 64), ActivatedAt: now.Add(-time.Hour), GraceUntil: now.Add(-time.Minute)}
			consumers := []rotation.ConsumerEvidence{{ConsumerID: "runner", Generation: 2, ObservedAt: now, EvidenceDigest: strings.Repeat("d", 64)}}
			raw, err := json.Marshal(struct {
				Activation rotation.ActivationEvidence
				Consumers  []rotation.ConsumerEvidence
			}{activation, consumers})
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			gateway.RotationActions = GitHubRotation{Plan: plan, Retirement: &GitHubRetirement{
				MutationID: req.MutationID, Target: identity, Client: client, Activation: activation, Consumers: consumers,
				VerifyActivation: func(context.Context, rotation.PreparationPlan, rotation.ActivationEvidence) error { return nil },
				VerifyConsumer:   func(context.Context, rotation.PreparationPlan, rotation.ConsumerEvidence) error { return nil },
				Authorize: func(context.Context, string, rotation.PreparationPlan, rotation.RetirementDecision) (rotation.RevocationGrant, error) {
					return rotation.RevocationGrant{PlanDigest: plan.ID, GrantDigest: req.GrantDigest, OldCredentialHandle: name,
						OldIdentityDigest: rotation.RevocationIdentityDigest(old), EvidenceDigest: hex.EncodeToString(digest[:]), ExpiresAt: now.Add(time.Minute)}, nil
				},
			}}
			envelope := effectgateway.Sign(req, key)
			result, err := gateway.Submit(ctx, envelope)
			if err != nil || result.Status != tc.status || result.ProviderCalls != tc.deletes || deletes != tc.deletes ||
				present["https://api.github.com"+path] != (tc.deletes == 0) {
				t.Fatalf("receipt=%+v deletes=%d secret present=%t error=%v", result, deletes, present["https://api.github.com"+path], err)
			}
			record, err := gateway.Journal.Status(req.MutationID)
			if err != nil || record.State != tc.state || result.OperationState != string(tc.state) {
				t.Fatalf("unexpected journal outcome: receipt=%s record=%s error=%v", result.OperationState, record.State, err)
			}
			if tc.name == "signer refuses" && (signs != 1 || fences != 0) {
				t.Fatalf("signer failure must prevent the final fence: signs=%d fences=%d", signs, fences)
			}
			if tc.name == "authority denies" && (fences != 1 || result.Note != "EXECUTION_FENCE_REJECTED") {
				t.Fatalf("late authority denial must retain the fence outcome: receipt=%+v fences=%d", result, fences)
			}
			status, err := http.NewRequestWithContext(ctx, http.MethodGet, issuerStatus, nil)
			if err != nil {
				t.Fatal(err)
			}
			issuerReply, err := (&http.Client{Transport: transport}).Do(status)
			if err != nil {
				t.Fatal(err)
			}
			issuerReply.Body.Close()
			if issuerReply.StatusCode != http.StatusOK {
				t.Fatal("repository secret deletion affected the issuer credential")
			}
			previousSigns, previousFences := signs, fences
			replay, err := gateway.Submit(ctx, envelope)
			if err != nil || replay.Status != effectgateway.StatusDuplicate || replay.ProviderCalls != 0 ||
				deletes != tc.deletes || signs != previousSigns || fences != previousFences {
				t.Fatalf("replay=%+v deletes=%d error=%v", replay, deletes, err)
			}
		})
	}
}
