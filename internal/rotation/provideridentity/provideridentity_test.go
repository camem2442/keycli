package provideridentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/camem2442/keycli/internal/rotation"
)

type mockRoundTrip func(*http.Request) (*http.Response, error)

func (m mockRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return m(req) }
func ok(s string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(s))}
}
func status(code int) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":"invalid token"}`))}
}
func readerFor(t *testing.T, f mockRoundTrip) Reader {
	t.Helper()
	r, e := NewReader(func(req *http.Request) error { req.Header.Set("Authorization", "Bearer synthetic-fixture"); return nil }, f)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func planFor(t *testing.T, provider, target string) rotation.PreparationPlan {
	t.Helper()
	old := rotation.Generation{Alias: "synthetic", Provider: provider, Kind: rotation.StaticServiceKey, Target: target, Number: 1}
	next := old
	next.Number = 2
	p, e := rotation.NewPreparationPlan(old, next, []rotation.Consumer{{ID: "runner", Required: true}})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func TestGitHubRepositorySecretIdentity(t *testing.T) {
	t0 := "2026-11-05T10:00:00Z"
	t1 := "2026-11-07T10:00:00Z"
	p := planFor(t, "github", "repo-identity")
	makeObserver := func(change string) *GitHubRepositorySecret {
		rt := readerFor(t, func(req *http.Request) (*http.Response, error) {
			if req.Method != "GET" || req.URL.Host != "api.github.com" || req.Header.Get("X-GitHub-Api-Version") != "2026-03-10" || req.Header.Get("Authorization") == "" {
				t.Fatal("untrusted GitHub call")
			}
			switch req.URL.Path {
			case "/repos/example/repo":
				if change == "wrong-repo" {
					return ok(`{"id":999,"full_name":"example/repo","owner":{"id":10,"login":"example"}}`), nil
				}
				return ok(`{"id":20,"full_name":"example/repo","owner":{"id":10,"login":"example"}}`), nil
			case "/repos/example/repo/actions/secrets/EXAMPLE_GEN_1":
				if change == "403" {
					return status(403), nil
				}
				u := t1
				if change == "changed-secret" {
					u = "2026-11-08T10:00:00Z"
				}
				return ok(fmt.Sprintf(`{"name":"EXAMPLE_GEN_1","created_at":%q,"updated_at":%q}`, t0, u)), nil
			default:
				t.Fatalf("unknown GitHub GET: %s", req.URL.Path)
				return nil, errors.New("unexpected")
			}
		})
		return &GitHubRepositorySecret{Reader: rt, PlanTarget: p.Old.Target, Repository: "example/repo", RepositoryID: 20, OwnerID: 10, SecretName: "EXAMPLE_GEN_1", ExpectedCreatedAt: t0, ExpectedUpdatedAt: t1}
	}
	id, e := makeObserver("").Verify(context.Background(), p, "EXAMPLE_GEN_1")
	if e != nil || id.Provider != "github" || id.ResourceID != "20" || id.CredentialID != "EXAMPLE_GEN_1" || len(id.ProofDigest) != 64 {
		t.Fatalf("valid provider metadata rejected %+v %v", id, e)
	}
	for _, c := range []string{"wrong-repo", "changed-secret", "403"} {
		t.Run(c, func(t *testing.T) {
			if _, e := makeObserver(c).Verify(context.Background(), p, "EXAMPLE_GEN_1"); e == nil {
				t.Fatal("incorrectly accepted GitHub identity")
			}
		})
	}
	if _, e := makeObserver("").Verify(context.Background(), p, "EXAMPLE_GEN_2"); e == nil {
		t.Fatal("wrong generation")
	}
	tampered := makeObserver("")
	tampered.ExpectedUpdatedAt = ""
	if _, e := tampered.Verify(context.Background(), p, "EXAMPLE_GEN_1"); e == nil {
		t.Fatal("missing pinned timestamp")
	}
}
func TestCloudflareAccountTokenIdentity(t *testing.T) {
	acct := strings.Repeat("a", 32)
	token := strings.Repeat("b", 32)
	issued := "2026-11-08T10:00:00Z"
	p := planFor(t, "cloudflare", "cf-account-token")
	policy := fmt.Sprintf(`[{"effect":"allow","resources":{"com.cloudflare.api.account.%s":"*"}}]`, acct)
	var jsonVal any
	if err := json.Unmarshal([]byte(policy), &jsonVal); err != nil {
		t.Fatal(err)
	}
	digest := shaMetadata(jsonVal)
	makeObserver := func(mode string) *CloudflareAccountToken {
		rt := readerFor(t, func(req *http.Request) (*http.Response, error) {
			if req.Method != "GET" || req.URL.Host != "api.cloudflare.com" || req.Header.Get("Authorization") == "" {
				t.Fatal("bad CF GET")
			}
			switch req.URL.Path {
			case "/client/v4/accounts/" + acct:
				return ok(fmt.Sprintf(`{"success":true,"result":{"id":%q}}`, acct)), nil
			case "/client/v4/accounts/" + acct + "/tokens/" + token:
				if mode == "403" {
					return status(403), nil
				}
				id := token
				st := "active"
				pol := policy
				if mode == "wrong-id" {
					id = strings.Repeat("c", 32)
				}
				if mode == "disabled" {
					st = "disabled"
				}
				if mode == "wildcard" {
					pol = `[{"effect":"allow","resources":{"com.cloudflare.api.account.*":"*"}}]`
				}
				return ok(fmt.Sprintf(`{"success":true,"result":{"id":%q,"name":"fixture-token","status":%q,"issued_on":%q,"policies":%s,"value":"synthetic-token-must-never-surface"}}`, id, st, issued, pol)), nil
			default:
				t.Fatal("unrecognised CF path")
				return nil, errors.New("unknown")
			}
		})
		return &CloudflareAccountToken{Reader: rt, PlanTarget: p.Old.Target, AccountID: acct, TokenID: token, ExpectedName: "fixture-token", ExpectedIssuedOn: issued, ExpectedPolicyDigest: digest}
	}
	id, e := makeObserver("").Verify(context.Background(), p, token)
	if e != nil || id.CredentialID != token || len(id.ProofDigest) != 64 || strings.Contains(fmt.Sprint(id), "synthetic-token") {
		t.Fatalf("valid CF metadata rejected %v %v", id, e)
	}
	for _, s := range []string{"403", "wrong-id", "disabled", "wildcard"} {
		t.Run(s, func(t *testing.T) {
			if _, e := makeObserver(s).Verify(context.Background(), p, token); e == nil {
				t.Fatal("wrong CF identity accepted")
			}
		})
	}
	if _, e := makeObserver("").Verify(context.Background(), p, strings.Repeat("c", 32)); e == nil {
		t.Fatal("wrong token accepted")
	}
}
func TestOracleVaultVersionIdentity(t *testing.T) {
	tenancy := "ocid1.tenancy.oc1..testtenant"
	compartment := "ocid1.compartment.oc1..compartment"
	vault := "ocid1.vault.oc1.aposaka.testvault"
	secret := "ocid1.vaultsecret.oc1.aposaka.testsecret"
	p := planFor(t, "oracle", "oracle-vault-secret")
	makeObserver := func(scenario string) *OracleVaultVersion {
		rt := readerFor(t, func(req *http.Request) (*http.Response, error) {
			if req.Method != "GET" || req.URL.Host != "vaults.ap-osaka-1.oci.oraclecloud.com" || req.Header.Get("Authorization") == "" {
				t.Fatal("wrong OCI origin or signer")
			}
			switch req.URL.Path {
			case "/20180608/secrets/" + secret:
				id := secret
				if scenario == "wrong-secret" {
					id = "ocid1.vaultsecret.oc1.aposaka.others"
				}
				return ok(fmt.Sprintf(`{"id":%q,"compartmentId":%q,"vaultId":%q,"lifecycleState":"ACTIVE"}`, id, compartment, vault)), nil
			case "/20180608/secrets/" + secret + "/version/7":
				if scenario == "403" {
					return status(403), nil
				}
				stages := `["DEPRECATED"]`
				if scenario == "still-current" {
					stages = `["CURRENT"]`
				}
				id := secret
				if scenario == "wrong-version-secret" {
					id = "ocid1.vaultsecret.oc1.aposaka.others"
				}
				deletion := "null"
				if scenario == "scheduled-deletion" {
					deletion = `"2026-11-20T10:00:00Z"`
				}
				return ok(fmt.Sprintf(`{"secretId":%q,"versionNumber":7,"stages":%s,"timeOfDeletion":%s,"timeCreated":"2026-11-01T10:00:00Z"}`, id, stages, deletion)), nil
			default:
				t.Fatal("bad OCI GET path " + req.URL.Path)
				return nil, errors.New("unknown")
			}
		})
		assert := func(_ context.Context, got string) error {
			if got != tenancy {
				return errors.New("wrong tenancy")
			}
			return nil
		}
		if scenario == "no-tenancy-proof" {
			assert = nil
		}
		return &OracleVaultVersion{Reader: rt, PlanTarget: p.Old.Target, TenancyID: tenancy, CompartmentID: compartment, VaultID: vault, SecretID: secret, Region: "ap-osaka-1", Version: 7, VerifySignerTenancy: assert}
	}
	id, e := makeObserver("").Verify(context.Background(), p, "7")
	if e != nil || id.CredentialID != "7" || id.AccountID != tenancy || len(id.ProofDigest) != 64 {
		t.Fatalf("valid OCI metadata rejected %v %v", id, e)
	}
	for _, s := range []string{"wrong-secret", "wrong-version-secret", "still-current", "scheduled-deletion", "403", "no-tenancy-proof"} {
		t.Run(s, func(t *testing.T) {
			if _, e := makeObserver(s).Verify(context.Background(), p, "7"); e == nil {
				t.Fatal("wrong OCI identity accepted")
			}
		})
	}
	if _, e := makeObserver("").Verify(context.Background(), p, "8"); e == nil {
		t.Fatal("wrong Vault version")
	}
}
func TestReaderRejectsSignerURLMutationAndNoAuthentication(t *testing.T) {
	if _, e := NewReader(nil, nil); e == nil {
		t.Fatal("missing authentication")
	}
	r, e := NewReader(func(req *http.Request) error { req.URL.Host = "evil.test"; return nil }, mockRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("signer mutated target but was sent")
		return nil, errors.New("unexpected")
	}))
	if e != nil {
		t.Fatal(e)
	}
	var result any
	if e := r.read(context.Background(), "https://api.github.com", "/repos/test/test", &result, nil); e == nil {
		t.Fatal("changed destination")
	}
}

// The observer signature is directly consumable by the existing K10-04 host
// callback. This is a synthetic provider transport; the real KeyCLI Journal
// requires an absolute private test directory. Neither outcome authenticates
// a real provider or authorizes live revocation.
var _ rotation.VerifyOldCredential = (GitHubRepositorySecret{}).Verify
var _ rotation.VerifyOldCredential = (CloudflareAccountToken{}).Verify
var _ rotation.VerifyOldCredential = (OracleVaultVersion{}).Verify

func TestGitHubProviderReadBindsFinalizeWithoutRevokerOnDrift(t *testing.T) {
	p := planFor(t, "github", "repo-finalize")
	now := time.Now()
	activation := rotation.ActivationEvidence{StageReceiptDigest: strings.Repeat("a", 64), PromotionReceiptDigest: strings.Repeat("b", 64), ActivatedAt: now.Add(-time.Hour), GraceUntil: now.Add(-time.Minute)}
	consumers := []rotation.ConsumerEvidence{{ConsumerID: "runner", Generation: 2, ObservedAt: now.Add(-time.Minute), EvidenceDigest: strings.Repeat("c", 64)}}
	evidence, _ := json.Marshal(struct {
		Activation rotation.ActivationEvidence
		Consumers  []rotation.ConsumerEvidence
	}{activation, consumers})
	evidenceSum := sha256.Sum256(evidence)
	for _, drift := range []bool{false, true} {
		t.Run(fmt.Sprintf("drift=%v", drift), func(t *testing.T) {
			reads := 0
			r := readerFor(t, func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/repos/example/repo":
					return ok(`{"id":20,"full_name":"example/repo","owner":{"id":10,"login":"example"}}`), nil
				case "/repos/example/repo/actions/secrets/EXAMPLE_GEN_1":
					reads++
					updated := "2026-11-07T10:00:00Z"
					if drift && reads > 1 {
						updated = "2026-11-08T10:00:00Z"
					}
					return ok(fmt.Sprintf(`{"name":"EXAMPLE_GEN_1","created_at":"2026-11-05T10:00:00Z","updated_at":%q}`, updated)), nil
				default:
					return status(404), nil
				}
			})
			observer := GitHubRepositorySecret{Reader: r, PlanTarget: p.Old.Target, Repository: "example/repo", RepositoryID: 20, OwnerID: 10, SecretName: "EXAMPLE_GEN_1", ExpectedCreatedAt: "2026-11-05T10:00:00Z", ExpectedUpdatedAt: "2026-11-07T10:00:00Z"}
			initial, e := observer.Verify(context.Background(), p, "EXAMPLE_GEN_1")
			if e != nil {
				t.Fatal(e)
			}
			initial.ObservedAt = time.Time{}
			metadata, _ := json.Marshal(initial)
			sum := sha256.Sum256(metadata)
			oldProof := hex.EncodeToString(sum[:])
			revoked := 0
			parent, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, 0700); err != nil {
				t.Fatal(err)
			}
			store := journal.Store{Root: filepath.Join(parent, "journal")}
			record, err := rotation.Finalize(context.Background(), store, "test-operation", p, activation, consumers, now,
				func(context.Context, rotation.PreparationPlan, rotation.ActivationEvidence) error { return nil },
				func(context.Context, rotation.PreparationPlan, rotation.ConsumerEvidence) error { return nil },
				func(context.Context, string, rotation.PreparationPlan, rotation.RetirementDecision) (rotation.RevocationGrant, error) {
					return rotation.RevocationGrant{PlanDigest: p.ID, GrantDigest: strings.Repeat("d", 64), OldCredentialHandle: "EXAMPLE_GEN_1", OldIdentityDigest: oldProof, EvidenceDigest: hex.EncodeToString(evidenceSum[:]), ExpiresAt: time.Now().Add(time.Minute)}, nil
				},
				observer.Verify,
				func(context.Context, rotation.Generation, string) (journal.Observation, error) {
					revoked++
					return journal.Observation{State: journal.Acknowledged, ProofDigest: strings.Repeat("e", 64)}, nil
				})
			if drift {
				if err == nil || revoked != 0 {
					t.Fatalf("drift accepted err %v revoked %d", err, revoked)
				}
				return
			}
			if err != nil || record.State != journal.Acknowledged || revoked != 1 {
				t.Fatalf("valid evidence did not reach revoker: %+v %v %d", record, err, revoked)
			}
		})
	}
}

func TestCloudflareRuntimeGroupRejectsTokenManagement(t *testing.T) {
	account, group := strings.Repeat("a", 32), strings.Repeat("b", 32)
	for _, tc := range []struct {
		name, scope string
		allowed     bool
	}{
		{"Workers Scripts Read", "com.cloudflare.api.account", true},
		{"Workers Scripts Write", "com.cloudflare.api.account", true},
		{"Account API Tokens Write", "com.cloudflare.api.account", false},
		{"Workers Scripts Read", "com.cloudflare.api.account.zone", false},
	} {
		t.Run(tc.name+tc.scope, func(t *testing.T) {
			reader := readerFor(t, func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.URL.Path != "/client/v4/accounts/"+account+"/tokens/permission_groups" {
					t.Fatal("permission inventory escaped its account")
				}
				return ok(fmt.Sprintf(`{"success":true,"result":[{"id":%q,"name":%q,"scopes":[%q],"is_selectable":true}]}`, group, tc.name, tc.scope)), nil
			})
			if err := reader.VerifyWorkersRuntimeGroup(context.Background(), account, group); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%t, error=%v", tc.allowed, err)
			}
		})
	}
}
