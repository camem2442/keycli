package provideridentity

import (
	"context"
	"errors"
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
	invalid := errors.New("Oracle Vault old version identity unavailable or changed")
	if p.Old.Provider != "oracle" || p.Old.Kind != rotation.StaticServiceKey || !validPlan(p) || p.Old.Target != o.PlanTarget ||
		!strings.HasPrefix(o.TenancyID, "ocid1.tenancy.") || !strings.HasPrefix(o.CompartmentID, "ocid1.") ||
		!strings.HasPrefix(o.VaultID, "ocid1.vault.") || !strings.HasPrefix(o.SecretID, "ocid1.vaultsecret.") ||
		!exactOCI(o.TenancyID) || !exactOCI(o.CompartmentID) || !exactOCI(o.VaultID) || !exactOCI(o.SecretID) ||
		o.Version == 0 || handle != strconv.FormatUint(o.Version, 10) || o.VerifySignerTenancy == nil || !regionValid(o.Region) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if err := o.VerifySignerTenancy(ctx, o.TenancyID); err != nil {
		return rotation.VerifiedOldCredential{}, invalid
	}
	origin := "https://vaults." + o.Region + ".oci.oraclecloud.com"
	path := "/20180608/secrets/" + o.SecretID
	var secret struct {
		ID             string `json:"id"`
		CompartmentID  string `json:"compartmentId"`
		VaultID        string `json:"vaultId"`
		LifecycleState string `json:"lifecycleState"`
	}
	if err := o.Reader.read(ctx, origin, path, &secret, nil); err != nil {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if secret.ID != o.SecretID || secret.VaultID != o.VaultID || secret.CompartmentID != o.CompartmentID || secret.LifecycleState != "ACTIVE" {
		return rotation.VerifiedOldCredential{}, invalid
	}
	var v struct {
		SecretID       string   `json:"secretId"`
		VersionNumber  uint64   `json:"versionNumber"`
		Stages         []string `json:"stages"`
		TimeOfDeletion *string  `json:"timeOfDeletion"`
		TimeCreated    string   `json:"timeCreated"`
	}
	if err := o.Reader.read(ctx, origin, path+"/version/"+handle, &v, nil); err != nil {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if v.SecretID != o.SecretID || v.VersionNumber != o.Version || v.TimeOfDeletion != nil || len(v.Stages) == 0 {
		return rotation.VerifiedOldCredential{}, invalid
	}
	deprecated := false
	for _, state := range v.Stages {
		if state == "DEPRECATED" {
			deprecated = true
		} else if state == "CURRENT" || state == "PENDING" || state == "PREVIOUS" || state == "LATEST" {
			return rotation.VerifiedOldCredential{}, invalid
		} else {
			return rotation.VerifiedOldCredential{}, invalid
		}
	}
	if !deprecated || !eqTime(v.TimeCreated, v.TimeCreated) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	return rotation.VerifiedOldCredential{Provider: "oracle", Kind: "oci-vault-secret-version", PlanDigest: p.ID, PlanTarget: p.Old.Target,
		AccountID: o.TenancyID, ResourceID: o.SecretID, CredentialID: handle, Generation: p.Old.Number, ObservedAt: time.Now().UTC(),
		ProofDigest: shaMetadata(struct {
			Tenancy, Compartment, Vault, Secret, Version, Created string
			Stages                                                []string
		}{o.TenancyID, o.CompartmentID, o.VaultID, o.SecretID, handle, v.TimeCreated, v.Stages})}, nil
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
func regionValid(s string) bool {
	if !strings.HasSuffix(s, "-1") && !strings.HasSuffix(s, "-2") && !strings.HasSuffix(s, "-3") {
		return false
	}
	return exactPart(s) && strings.Count(s, "-") >= 2 && len(s) < 40
}
