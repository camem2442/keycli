package providerlifecycle

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// StageOraclePendingVaultVersion uses the OCI MANAGEMENT endpoint UpdateSecret.
// It creates a PENDING version, not CURRENT. The host supplies the secret bytes
// and a signer for this exact request, plus the independently observed ETag.
// Creation acknowledgement alone does not identify or verify the new version.
func (c Client) StageOraclePendingVaultVersion(ctx context.Context, gate HostAuthorize, region, secretID, etag string, raw []byte) (Receipt, error) {
	if !regionID.MatchString(region) || !validOCI(secretID, "vaultsecret") || invalidETag(etag) || len(raw) == 0 || len(raw) > 25*1024 {
		return Receipt{}, errors.New("invalid OCI pending-version target")
	}
	target := region + "/" + secretID
	if err := authorize(ctx, gate, "oracle", "stage-pending-vault-version", target); err != nil {
		return Receipt{}, err
	}
	payload, _ := json.Marshal(map[string]any{"secretContent": map[string]string{
		"contentType": "BASE64", "content": base64.StdEncoding.EncodeToString(raw), "stage": "PENDING"}})
	defer func() {
		for i := range payload {
			payload[i] = 0
		}
	}()
	// OCI requires If-Match; signer MUST sign the immutable method/url/body.
	data, err := c.callHeaders(ctx, http.MethodPut, "https://vaults."+region+".oci.oraclecloud.com", "/20180608/secrets/"+secretID, payload, map[string]string{"If-Match": etag}, http.StatusOK)
	if err != nil {
		return Receipt{}, err
	}
	var obj struct {
		ID             string `json:"id"`
		LifecycleState string `json:"lifecycleState"`
	}
	if json.Unmarshal(data, &obj) != nil || obj.ID != secretID || obj.LifecycleState != "ACTIVE" {
		return Receipt{}, errors.New("OCI staging response identity unknown")
	}
	return receipt("oracle", "stage-pending-vault-version", region, secretID), nil
}

// ScheduleOracleOldVersionDeletion applies ONLY after the Finalize host has
// verified the DEPRECATED old version, all consumer exits and rollback grace.
// Oracle deletion scheduling may IMMEDIATELY make the version unusable; the
// requested permanent deletion date (1..30 days) is NOT a safety grace period.
func (c Client) ScheduleOracleOldVersionDeletion(ctx context.Context, gate HostAuthorize, region, secretID, etag string, version uint64, deleteAt time.Time) (Receipt, error) {
	if !regionID.MatchString(region) || !validOCI(secretID, "vaultsecret") || invalidETag(etag) || version == 0 || deleteAt.Before(time.Now().Add(24*time.Hour)) || deleteAt.After(time.Now().Add(30*24*time.Hour)) {
		return Receipt{}, errors.New("invalid OCI deprecated-version deletion request")
	}
	name := strconv.FormatUint(version, 10)
	target := region + "/" + secretID + "/version/" + name
	if err := authorize(ctx, gate, "oracle", "schedule-deprecated-vault-version-deletion", target); err != nil {
		return Receipt{}, err
	}
	body, _ := json.Marshal(map[string]string{"timeOfDeletion": deleteAt.UTC().Format(time.RFC3339)})
	_, err := c.callHeaders(ctx, http.MethodPost, "https://vaults."+region+".oci.oraclecloud.com", "/20180608/secrets/"+secretID+"/version/"+name+"/actions/scheduleDeletion", body, map[string]string{"If-Match": etag}, http.StatusNoContent)
	if err != nil {
		return Receipt{}, err
	}
	return receipt("oracle", "schedule-deprecated-vault-version-deletion", secretID, name), nil
}

func invalidETag(etag string) bool {
	if etag == "" || len(etag) > 128 {
		return true
	}
	return strings.ContainsAny(etag, "\r\n\x00\t")
}
