package rotation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// VerifiedOldCredential is provider metadata observed by a trusted host.
// It contains no secret value. The caller MUST independently authenticate
// the observation against the issuer/account and target before returning it.
// Neither this structure nor its digest proves the host is trustworthy.
type VerifiedOldCredential struct {
	Provider     string
	Kind         string
	PlanDigest   string
	PlanTarget   string
	AccountID    string
	ResourceID   string
	CredentialID string
	Generation   uint64
	ObservedAt   time.Time
	ProofDigest  string
}

// VerifyOldCredential must read an authenticated provider/issuer inventory,
// not a caller-supplied string or the KeyCLI journal. It runs immediately
// before the high-risk revoker is admitted and cannot issue a new credential.
type VerifyOldCredential func(context.Context, PreparationPlan, string) (VerifiedOldCredential, error)

var (
	hexAccountID           = regexp.MustCompile(`^[a-f0-9]{32}$`)
	githubNumericID        = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
	githubGenerationSecret = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,90}_GEN_[1-9][0-9]*$`)
	vaultSecretOCID        = regexp.MustCompile(`^ocid1\.vaultsecret\.[A-Za-z0-9._-]{3,256}$`)
	tenancyOCID            = regexp.MustCompile(`^ocid1\.tenancy\.[A-Za-z0-9._-]{3,256}$`)
)

// revocationIdentityDigest is a grant binding, not a token fingerprint or
// a producer attestation. Never persist provider access tokens in this input.
func revocationIdentityDigest(id VerifiedOldCredential) string {
	id.ObservedAt = time.Time{} // fresh provider GET may have a different observation time
	b, _ := json.Marshal(id)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func validOldCredentialIdentity(plan PreparationPlan, expectedHandle string, id VerifiedOldCredential, now time.Time) error {
	if id.PlanDigest != plan.ID || id.PlanTarget != plan.Old.Target ||
		id.Provider != plan.Old.Provider || id.Generation != plan.Old.Number ||
		id.CredentialID != expectedHandle || !namePattern.MatchString(expectedHandle) ||
		!digestPattern.MatchString(id.ProofDigest) || id.ObservedAt.IsZero() ||
		id.ObservedAt.After(now.Add(30*time.Second)) || now.Sub(id.ObservedAt) > 15*time.Minute {
		return errors.New("old provider credential identity is not bound to the plan")
	}
	switch id.Provider {
	case "github":
		// This kind deletes one generation-specific *GitHub Actions Secret*.
		// It NEVER revokes the upstream credential whose value was stored.
		if id.Kind != "github-actions-secret" || !githubNumericID.MatchString(id.AccountID) ||
			!githubNumericID.MatchString(id.ResourceID) ||
			!githubGenerationSecret.MatchString(id.CredentialID) ||
			!strings.HasSuffix(id.CredentialID, "_GEN_"+strconv.FormatUint(plan.Old.Number, 10)) {
			return errors.New("GitHub old secret identity is ambiguous")
		}
	case "cloudflare":
		// Only *account API token* revocation here. Worker secrets/version
		// promotion belong to Cloudflare Delivery, not this revoker.
		if id.Kind != "cloudflare-account-token" || !hexAccountID.MatchString(id.AccountID) ||
			id.ResourceID != id.AccountID || !hexAccountID.MatchString(id.CredentialID) {
			return errors.New("Cloudflare token/account identity is ambiguous")
		}
	case "oracle":
		// OCI Vault secret version is identified by exact tenancy + secret
		// OCID + positive version number, not a made-up version OCID.
		v, err := strconv.ParseUint(id.CredentialID, 10, 64)
		if id.Kind != "oci-vault-secret-version" || !tenancyOCID.MatchString(id.AccountID) ||
			!vaultSecretOCID.MatchString(id.ResourceID) || err != nil || v == 0 ||
			strconv.FormatUint(v, 10) != id.CredentialID {
			return errors.New("OCI Vault version identity is ambiguous")
		}
	default:
		return errors.New("provider does not support exact static credential revocation")
	}
	return nil
}
