package provideridentity

import (
	"context"
	"errors"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/rotation"
)

// AssertTenancy is a separate trusted-host check of OCI signer tenancy.
// The Vault Secret Management response itself has no tenancy-ID field.
type AssertTenancy func(context.Context, string) error

// OracleVaultVersion uses only *management* GET endpoints. It never calls
// Secret Retrieval API/GetSecretBundle, reads contents, or schedules deletion.
// A DEPRECATED version with no deletion time is eligible for host review; this
// metadata observer alone cannot authorise or execute that deletion.
type OracleVaultVersion struct {
	Reader                                              Reader
	PlanTarget                                          string
	TenancyID, CompartmentID, VaultID, SecretID, Region string
	Version                                             uint64
	VerifySignerTenancy                                 AssertTenancy
}

func (o OracleVaultVersion) Verify(ctx context.Context, p rotation.PreparationPlan, handle string) (rotation.VerifiedOldCredential, error) {
	return o.verify(ctx, p, handle, nil, "")
}

// VerifyWithETag verifies the exact old version and checks the host-pinned
// ETag against the version GET, rather than the parent secret GET.
func (o OracleVaultVersion) VerifyWithETag(ctx context.Context, p rotation.PreparationPlan, handle, expectedETag string) (rotation.VerifiedOldCredential, error) {
	if !validOracleETag(expectedETag) {
		return rotation.VerifiedOldCredential{}, errors.New("invalid Oracle Vault version ETag")
	}
	return o.verify(ctx, p, handle, nil, expectedETag)
}

// VerifyScheduledDeletion corroborates the exact scheduled time using the
// same independent management reads as Verify.
func (o OracleVaultVersion) VerifyScheduledDeletion(ctx context.Context, p rotation.PreparationPlan, handle string, deleteAt time.Time) (rotation.VerifiedOldCredential, error) {
	return o.verify(ctx, p, handle, &deleteAt, "")
}

// VerifyScheduledDeletionFromETag binds independent schedule evidence to the
// original write precondition. A new ETag after the write is expected.
func (o OracleVaultVersion) VerifyScheduledDeletionFromETag(ctx context.Context, p rotation.PreparationPlan, handle string, deleteAt time.Time, originalETag string) (rotation.VerifiedOldCredential, error) {
	if !validOracleETag(originalETag) {
		return rotation.VerifiedOldCredential{}, errors.New("invalid Oracle Vault original ETag")
	}
	proof, err := o.VerifyScheduledDeletion(ctx, p, handle, deleteAt)
	if err != nil {
		return rotation.VerifiedOldCredential{}, err
	}
	proof.ProofDigest = o.oracleVersionETagProof(proof.ProofDigest, originalETag)
	return proof, nil
}

func (o OracleVaultVersion) verify(ctx context.Context, p rotation.PreparationPlan, handle string, deleteAt *time.Time, expectedETag string) (rotation.VerifiedOldCredential, error) {
	invalid := errors.New("Oracle Vault old version identity unavailable or changed")
	if p.Old.Provider != "oracle" || p.Old.Kind != rotation.StaticServiceKey || !validPlan(p) || p.Old.Target != o.PlanTarget ||
		!validOracleVersion(o.Version) || handle != strconv.FormatUint(o.Version, 10) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if _, err := o.readSecret(ctx); err != nil {
		return rotation.VerifiedOldCredential{}, invalid
	}
	origin, path := o.managementEndpoint()
	var v struct {
		SecretID       string   `json:"secretId"`
		VersionNumber  uint64   `json:"versionNumber"`
		Stages         []string `json:"stages"`
		TimeOfDeletion *string  `json:"timeOfDeletion"`
		TimeCreated    string   `json:"timeCreated"`
	}
	var headers http.Header
	if err := o.Reader.readWithResponseHeaders(ctx, origin, path+"/version/"+handle, &v, nil, &headers); err != nil ||
		(expectedETag != "" && !oracleETagMatches(headers, expectedETag)) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if v.SecretID != o.SecretID || v.VersionNumber != o.Version ||
		(v.TimeOfDeletion == nil) != (deleteAt == nil) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if deleteAt != nil {
		observed, err := fixedTime(*v.TimeOfDeletion)
		if err != nil || !observed.Equal(*deleteAt) {
			return rotation.VerifiedOldCredential{}, invalid
		}
	}
	if len(v.Stages) != 1 || v.Stages[0] != "DEPRECATED" || !eqTime(v.TimeCreated, v.TimeCreated) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	proof := rotation.VerifiedOldCredential{Provider: "oracle", Kind: "oci-vault-secret-version", PlanDigest: p.ID, PlanTarget: p.Old.Target,
		AccountID: o.TenancyID, ResourceID: o.SecretID, CredentialID: handle, Generation: p.Old.Number, ObservedAt: time.Now().UTC(),
		ProofDigest: shaMetadata(struct {
			Tenancy, Compartment, Vault, Secret, Version, Created string
			Stages                                                []string
		}{o.TenancyID, o.CompartmentID, o.VaultID, o.SecretID, handle, v.TimeCreated, v.Stages})}
	if expectedETag != "" {
		proof.ProofDigest = o.oracleVersionETagProof(proof.ProofDigest, expectedETag)
	}
	return proof, nil
}
func (o OracleVaultVersion) oracleVersionETagProof(digest, etag string) string {
	return shaMetadata([3]string{digest, o.Region, etag})
}

func exactOCI(s string) bool {
	if len(s) < 12 || len(s) > 300 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

var oracleRegion = regexp.MustCompile(`^[a-z]{2}-[a-z]+-[1-9]$`)

func regionValid(s string) bool { return len(s) < 40 && oracleRegion.MatchString(s) }

func validOracleVersion(n uint64) bool { return n > 0 && n <= math.MaxInt64 }

// OraclePendingSnapshot is an opaque snapshot obtained by PreflightPending.
// The host keeps it with the operation until it verifies the mutation.
type OraclePendingSnapshot struct {
	target      [6]string
	etag        string
	versionName string
	current     uint64
	versions    map[uint64]oracleObservedVersion
}

type oracleVersionSummary struct {
	SecretID      string   `json:"secretId"`
	VersionNumber uint64   `json:"versionNumber"`
	Name          string   `json:"name"`
	TimeCreated   string   `json:"timeCreated"`
	TimeDeleted   *string  `json:"timeOfDeletion"`
	Stages        []string `json:"stages"`
}

type oracleObservedVersion struct {
	Name, Created, Deletion, Role string
}

var errOraclePending = errors.New("Oracle Vault pending version identity unavailable or changed")

// PreflightPending binds the expected ETag and current version to independent
// management reads. The version name is the marker the host will put in
// UpdateSecret.secretContent.name; it must not have been used on this secret.
func (o OracleVaultVersion) PreflightPending(ctx context.Context, expectedETag, versionName string) (OraclePendingSnapshot, error) {
	if !validOracleETag(expectedETag) || !exactPart(versionName) || !validOracleVersion(o.Version) {
		return OraclePendingSnapshot{}, errOraclePending
	}
	headers, err := o.readSecret(ctx)
	if err != nil || !oracleETagMatches(headers, expectedETag) {
		return OraclePendingSnapshot{}, errOraclePending
	}
	versions, current, err := o.listVersions(ctx)
	if err != nil || current != o.Version {
		return OraclePendingSnapshot{}, errOraclePending
	}
	for _, v := range versions {
		if v.Name == versionName || v.Role == "PENDING" {
			return OraclePendingSnapshot{}, errOraclePending
		}
	}
	return OraclePendingSnapshot{target: o.snapshotTarget(), etag: expectedETag, versionName: versionName, current: current, versions: versions}, nil
}

// VerifyPending identifies exactly one new Pending version bearing the
// original operation marker. It also proves the pinned Current is unchanged.
// A write acknowledgement is neither an input nor an identity proof.
func (o OracleVaultVersion) VerifyPending(ctx context.Context, before OraclePendingSnapshot) (uint64, string, error) {
	if before.target != o.snapshotTarget() || before.current != o.Version || len(before.versions) == 0 {
		return 0, "", errOraclePending
	}
	if _, err := o.readSecret(ctx); err != nil {
		return 0, "", errOraclePending
	}
	after, current, err := o.listVersions(ctx)
	if err != nil || current != before.current || len(after) != len(before.versions)+1 {
		return 0, "", errOraclePending
	}
	for id, observed := range before.versions {
		if after[id] != observed {
			return 0, "", errOraclePending
		}
	}
	for id, v := range after {
		if _, existed := before.versions[id]; existed {
			continue
		}
		if v.Name != before.versionName || v.Role != "PENDING" || v.Deletion != "" {
			return 0, "", errOraclePending
		}
		return id, shaMetadata(struct {
			Target           [6]string
			ETag, Operation  string
			Current, Version uint64
			Before, After    map[uint64]oracleObservedVersion
		}{before.target, before.etag, before.versionName, current, id, before.versions, after}), nil
	}
	return 0, "", errOraclePending
}

func (o OracleVaultVersion) snapshotTarget() [6]string {
	return [6]string{o.Region, o.TenancyID, o.CompartmentID, o.VaultID, o.SecretID, o.PlanTarget}
}

func (o OracleVaultVersion) managementEndpoint() (string, string) {
	return "https://vaults." + o.Region + ".oci.oraclecloud.com", "/20180608/secrets/" + o.SecretID
}

func (o OracleVaultVersion) readSecret(ctx context.Context) (http.Header, error) {
	if !strings.HasPrefix(o.TenancyID, "ocid1.tenancy.") || !strings.HasPrefix(o.CompartmentID, "ocid1.") ||
		!strings.HasPrefix(o.VaultID, "ocid1.vault.") || !strings.HasPrefix(o.SecretID, "ocid1.vaultsecret.") ||
		!exactOCI(o.TenancyID) || !exactOCI(o.CompartmentID) || !exactOCI(o.VaultID) || !exactOCI(o.SecretID) ||
		!regionValid(o.Region) || o.VerifySignerTenancy == nil || o.VerifySignerTenancy(ctx, o.TenancyID) != nil {
		return nil, errors.New("Oracle Vault target identity unavailable")
	}
	origin, path := o.managementEndpoint()
	var secret struct {
		ID             string `json:"id"`
		CompartmentID  string `json:"compartmentId"`
		VaultID        string `json:"vaultId"`
		LifecycleState string `json:"lifecycleState"`
	}
	var headers http.Header
	if err := o.Reader.readWithResponseHeaders(ctx, origin, path, &secret, nil, &headers); err != nil ||
		secret.ID != o.SecretID || secret.VaultID != o.VaultID || secret.CompartmentID != o.CompartmentID || secret.LifecycleState != "ACTIVE" {
		return nil, errors.New("Oracle Vault target identity unavailable")
	}
	return headers, nil
}

func (o OracleVaultVersion) listVersions(ctx context.Context) (map[uint64]oracleObservedVersion, uint64, error) {
	origin, path := o.managementEndpoint()
	var summaries []oracleVersionSummary
	var headers http.Header
	// Refuse partial inventories. A caller may use a secret whose entire
	// inventory fits in one provider response; no continuation token is used.
	if err := o.Reader.readWithResponseHeaders(ctx, origin, path+"/versions", &summaries, nil, &headers); err != nil ||
		len(headers.Values("opc-next-page")) != 0 {
		return nil, 0, errOraclePending
	}
	versions := make(map[uint64]oracleObservedVersion, len(summaries))
	names := make(map[string]bool, len(summaries))
	var current uint64
	for _, v := range summaries {
		created, err := fixedTime(v.TimeCreated)
		role, validStages := oracleVersionRole(v.Stages)
		if v.SecretID != o.SecretID || !validOracleVersion(v.VersionNumber) || err != nil || !validStages ||
			(v.Name != "" && names[v.Name]) {
			return nil, 0, errOraclePending
		}
		if _, duplicate := versions[v.VersionNumber]; duplicate {
			return nil, 0, errOraclePending
		}
		if role == "CURRENT" {
			if current != 0 || v.TimeDeleted != nil {
				return nil, 0, errOraclePending
			}
			current = v.VersionNumber
		}
		var deletion string
		if v.TimeDeleted != nil {
			deletedAt, err := fixedTime(*v.TimeDeleted)
			if err != nil {
				return nil, 0, errOraclePending
			}
			deletion = deletedAt.Format(time.RFC3339Nano)
		}
		names[v.Name] = true
		versions[v.VersionNumber] = oracleObservedVersion{v.Name, created.Format(time.RFC3339Nano), deletion, role}
	}
	if current == 0 {
		return nil, 0, errOraclePending
	}
	return versions, current, nil
}

func oracleVersionRole(stages []string) (string, bool) {
	var role string
	var latest bool
	for _, stage := range stages {
		switch stage {
		case "LATEST":
			if latest {
				return "", false
			}
			latest = true
		case "CURRENT", "PENDING", "PREVIOUS", "DEPRECATED":
			if role != "" {
				return "", false
			}
			role = stage
		default:
			return "", false
		}
	}
	return role, role != ""
}

func validOracleETag(s string) bool {
	if s == "" || len(s) > 128 || s == "*" || strings.HasPrefix(s, "W/") {
		return false
	}
	for _, c := range s {
		if c <= ' ' || c > '~' || c == ',' {
			return false
		}
	}
	return true
}

func oracleETagMatches(headers http.Header, expected string) bool {
	values := headers.Values("ETag")
	return len(values) == 1 && values[0] == expected
}

// RecoverPending observes an already-dispatched operation after its in-memory
// snapshot was lost. The caller must first authenticate the original journal
// intent and its unique version name. A proof from this path does not assert
// that it has compared the complete inventory with a preflight snapshot.
func (o OracleVaultVersion) RecoverPending(ctx context.Context, versionName string) (uint64, string, error) {
	if !exactPart(versionName) || !validOracleVersion(o.Version) {
		return 0, "", errOraclePending
	}
	if _, err := o.readSecret(ctx); err != nil {
		return 0, "", errOraclePending
	}
	versions, current, err := o.listVersions(ctx)
	if err != nil || current != o.Version {
		return 0, "", errOraclePending
	}
	var candidate uint64
	for id, v := range versions {
		if v.Role == "PENDING" {
			if candidate != 0 || v.Name != versionName || v.Deletion != "" {
				return 0, "", errOraclePending
			}
			candidate = id
		}
	}
	if candidate == 0 {
		return 0, "", errOraclePending
	}
	return candidate, o.oracleObservationDigest("recovered-pending", versionName, candidate, versions), nil
}

// VerifyCurrent observes a promotion already performed on a separately
// approved path. It cannot promote a secret version.
func (o OracleVaultVersion) VerifyCurrent(ctx context.Context, version uint64, versionName string) (string, error) {
	if !exactPart(versionName) || !validOracleVersion(o.Version) || !validOracleVersion(version) || version == o.Version {
		return "", errOraclePending
	}
	if _, err := o.readSecret(ctx); err != nil {
		return "", errOraclePending
	}
	versions, current, err := o.listVersions(ctx)
	if err != nil || current != version || versions[current].Name != versionName {
		return "", errOraclePending
	}
	former := versions[o.Version]
	if former.Role != "PREVIOUS" && former.Role != "DEPRECATED" {
		return "", errOraclePending
	}
	return o.oracleObservationDigest("current", versionName, current, versions), nil
}

func (o OracleVaultVersion) oracleObservationDigest(state, name string, id uint64, versions map[uint64]oracleObservedVersion) string {
	return shaMetadata(struct {
		Target            [6]string
		State, Name       string
		Original, Version uint64
		Versions          map[uint64]oracleObservedVersion
	}{o.snapshotTarget(), state, name, o.Version, id, versions})
}
