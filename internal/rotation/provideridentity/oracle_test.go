package provideridentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
)

const (
	oracleTestSecret = "ocid1.vaultsecret.oc1.aposaka.testsecret"
	oracleTestETag   = `"pinned"`
	oracleTestMarker = "kc-synthetic-mutation"
)

func oracleReaderFixture(t *testing.T, testHTTP func(*http.Request) *http.Response) OracleVaultVersion {
	t.Helper()
	o := OracleVaultVersion{
		TenancyID: "ocid1.tenancy.oc1..testtenant", CompartmentID: "ocid1.compartment.oc1..compartment",
		VaultID: "ocid1.vault.oc1.aposaka.testvault", SecretID: oracleTestSecret,
		Region: "ap-osaka-1", Version: 7, PlanTarget: "oracle-vault-secret",
	}
	o.VerifySignerTenancy = func(_ context.Context, id string) error {
		if id != "ocid1.tenancy.oc1..testtenant" {
			return errors.New("wrong signer tenancy")
		}
		return nil
	}
	o.Reader = readerFor(t, func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Host != "vaults.ap-osaka-1.oci.oraclecloud.com" || r.URL.RawQuery != "" {
			t.Fatalf("unexpected OCI request: %s %s", r.Method, r.URL)
		}
		return testHTTP(r), nil
	})
	return o
}

func oracleSecretFixture() *http.Response {
	r := ok(fmt.Sprintf(`{"id":%q,"compartmentId":"ocid1.compartment.oc1..compartment","vaultId":"ocid1.vault.oc1.aposaka.testvault","lifecycleState":"ACTIVE"}`, oracleTestSecret))
	r.Header.Set("ETag", oracleTestETag)
	return r
}

func versionFixture(id uint64, name string, roles ...string) oracleVersionSummary {
	return oracleVersionSummary{SecretID: oracleTestSecret, VersionNumber: id, Name: name, Stages: roles, TimeCreated: "2026-11-11T10:00:00Z"}
}

func TestOracleObservePending(t *testing.T) {
	ctx := context.Background()
	current := versionFixture(7, "old", "CURRENT")
	pending := versionFixture(8, oracleTestMarker, "PENDING", "LATEST")
	for _, tt := range []struct {
		name       string
		after      []oracleVersionSummary
		page, want bool
	}{
		{"exact operation", []oracleVersionSummary{pending, current}, false, true},
		{"no effect", []oracleVersionSummary{current}, false, false},
		{"unrelated pending", []oracleVersionSummary{current, versionFixture(8, "other", "PENDING")}, false, false},
		{"multiple candidates", []oracleVersionSummary{current, pending, versionFixture(9, "other", "PENDING")}, false, false},
		{"duplicate version", []oracleVersionSummary{pending, pending}, false, false},
		{"current moved", []oracleVersionSummary{versionFixture(7, "old", "PREVIOUS"), versionFixture(8, oracleTestMarker, "CURRENT")}, false, false},
		{"current conflated with pending", []oracleVersionSummary{current, versionFixture(8, oracleTestMarker, "PENDING", "CURRENT")}, false, false},
		{"partial inventory", []oracleVersionSummary{current, pending}, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			listCalls := 0
			o := oracleReaderFixture(t, func(r *http.Request) *http.Response {
				switch r.URL.Path {
				case "/20180608/secrets/" + oracleTestSecret:
					return oracleSecretFixture()
				case "/20180608/secrets/" + oracleTestSecret + "/versions":
					listCalls++
					summaries := tt.after
					if listCalls == 1 {
						summaries = []oracleVersionSummary{versionFixture(7, "old", "CURRENT", "LATEST")}
					}
					body, _ := json.Marshal(summaries)
					resp := ok(string(body))
					if tt.page && listCalls > 1 {
						resp.Header.Set("opc-next-page", "more")
					}
					return resp
				default:
					t.Fatalf("unexpected OCI path: %s", r.URL.Path)
					return nil
				}
			})
			before, err := o.PreflightPending(ctx, oracleTestETag, oracleTestMarker)
			if err != nil {
				t.Fatal(err)
			}
			differentRegion := o
			differentRegion.Region = "us-ashburn-1"
			if _, _, err := differentRegion.VerifyPending(ctx, before); err == nil {
				t.Fatal("accepted snapshot from another region")
			}
			id, digest, err := o.VerifyPending(ctx, before)
			if tt.want && (err != nil || id != 8 || len(digest) != 64) {
				t.Fatalf("valid Pending rejected: id=%d proof=%q error=%v", id, digest, err)
			}
			if !tt.want && (err == nil || id != 0 || digest != "") {
				t.Fatal("ambiguous observation accepted")
			}
		})
	}
}

func TestOracleETagsAndTenancy(t *testing.T) {
	ctx := context.Background()
	versionETag := `"version"`
	o := oracleReaderFixture(t, func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/20180608/secrets/" + oracleTestSecret:
			return oracleSecretFixture()
		case "/20180608/secrets/" + oracleTestSecret + "/version/7":
			b, _ := json.Marshal(versionFixture(7, "old", "DEPRECATED"))
			resp := ok(string(b))
			resp.Header.Set("ETag", versionETag)
			return resp
		default:
			t.Fatalf("GET not allowed after mismatched ETag or tenancy: %s", r.URL.Path)
			return nil
		}
	})
	p := planFor(t, "oracle", o.PlanTarget)
	if _, err := o.VerifyWithETag(ctx, p, "7", versionETag); err != nil {
		t.Fatal(err)
	}
	if _, err := o.VerifyWithETag(ctx, p, "7", oracleTestETag); err == nil {
		t.Fatal("accepted parent secret ETag as version ETag")
	}
	if _, err := o.PreflightPending(ctx, versionETag, oracleTestMarker); err == nil {
		t.Fatal("accepted version ETag for parent secret")
	}
	o.TenancyID = "ocid1.tenancy.oc1..other"
	if _, err := o.VerifyWithETag(ctx, p, "7", versionETag); err == nil {
		t.Fatal("accepted unverified tenancy")
	}
}

func TestOracleRecoveryAndPromotionObservation(t *testing.T) {
	ctx := context.Background()
	promoted := false
	o := oracleReaderFixture(t, func(r *http.Request) *http.Response {
		switch r.URL.Path {
		case "/20180608/secrets/" + oracleTestSecret:
			return oracleSecretFixture()
		case "/20180608/secrets/" + oracleTestSecret + "/versions":
			versions := []oracleVersionSummary{versionFixture(7, "old", "CURRENT"), versionFixture(8, oracleTestMarker, "PENDING", "LATEST")}
			if promoted {
				versions = []oracleVersionSummary{versionFixture(7, "old", "PREVIOUS"), versionFixture(8, oracleTestMarker, "CURRENT", "LATEST")}
			}
			b, _ := json.Marshal(versions)
			return ok(string(b))
		default:
			t.Fatalf("unexpected OCI path: %s", r.URL.Path)
			return nil
		}
	})
	if _, err := o.VerifyCurrent(ctx, 8, oracleTestMarker); err == nil {
		t.Fatal("Pending accepted as Current")
	}
	if _, err := o.PreflightPending(ctx, oracleTestETag, oracleTestMarker); err == nil {
		t.Fatal("accepted an existing Pending version as a new operation")
	}
	if id, digest, err := o.RecoverPending(ctx, oracleTestMarker); err != nil || id != 8 || len(digest) != 64 {
		t.Fatalf("recovery failed: %v", err)
	}
	promoted = true
	if _, _, err := o.RecoverPending(ctx, oracleTestMarker); err == nil {
		t.Fatal("Current accepted as Pending")
	}
	if digest, err := o.VerifyCurrent(ctx, 8, oracleTestMarker); err != nil || len(digest) != 64 {
		t.Fatalf("approved promotion could not be observed: %v", err)
	}
	if _, err := o.VerifyCurrent(ctx, 8, "other"); err == nil {
		t.Fatal("wrong operation marker accepted")
	}
}
