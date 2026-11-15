package providerlifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const oraclePendingAction = "stage-pending-vault-version"
const oracleScheduleAction = "schedule-deprecated-vault-version-deletion"

var oracleVersionName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)

// ErrOracleEffectUnknown means HTTP dispatch was attempted and the adapter could
// not verify its acknowledgement. Reconcile the exact operation before retrying.
var ErrOracleEffectUnknown = errors.New("OCI effect response unknown")

// StageOraclePendingVaultVersion preserves the unnamed PENDING stage adapter.
// The host's admission callback is checked again immediately before dispatch.
func (c Client) StageOraclePendingVaultVersion(ctx context.Context, gate HostAuthorize, region, secretID, etag string, raw []byte) (Receipt, error) {
	return c.stageOraclePendingVaultVersion(ctx, gate, region, secretID, etag, "", raw, nil)
}

// StageOraclePendingVaultVersionWithFence uses OCI UpdateSecret to create a
// named PENDING version. Creation acknowledgement does not verify the new version.
func (c Client) StageOraclePendingVaultVersionWithFence(ctx context.Context, gate HostAuthorize, region, secretID, etag, versionName string, raw []byte, beforeSend func(context.Context) error) (Receipt, error) {
	if !oracleVersionName.MatchString(versionName) || beforeSend == nil {
		return Receipt{}, errors.New("OCI pending version name and final fence required")
	}
	return c.stageOraclePendingVaultVersion(ctx, gate, region, secretID, etag, versionName, raw, beforeSend)
}

func (c Client) stageOraclePendingVaultVersion(ctx context.Context, gate HostAuthorize, region, secretID, etag, versionName string, raw []byte, beforeSend func(context.Context) error) (Receipt, error) {
	if !regionID.MatchString(region) || !validOCI(secretID, "vaultsecret") || invalidETag(etag) || len(raw) == 0 || len(raw) > 25*1024 {
		return Receipt{}, errors.New("invalid OCI pending-version target")
	}
	target := region + "/" + secretID
	if err := authorize(ctx, gate, "oracle", oraclePendingAction, target); err != nil {
		return Receipt{}, err
	}
	if beforeSend == nil {
		beforeSend = func(ctx context.Context) error {
			return authorize(ctx, gate, "oracle", oraclePendingAction, target)
		}
	}
	content := map[string]string{
		"contentType": "BASE64", "content": base64.StdEncoding.EncodeToString(raw), "stage": "PENDING",
	}
	if versionName != "" {
		content["name"] = versionName
	}
	payload, _ := json.Marshal(map[string]any{"secretContent": content})
	defer clear(payload)

	data, err := c.callOracle(ctx, http.MethodPut, region, "/20180608/secrets/"+secretID, etag, payload, beforeSend, http.StatusOK)
	if err != nil {
		return Receipt{}, err
	}
	var obj struct {
		ID             string `json:"id"`
		LifecycleState string `json:"lifecycleState"`
	}
	if json.Unmarshal(data, &obj) != nil || obj.ID != secretID || obj.LifecycleState != "ACTIVE" {
		return Receipt{}, fmt.Errorf("%w: staging response identity unknown", ErrOracleEffectUnknown)
	}
	return receipt("oracle", oraclePendingAction, region, secretID), nil
}

// ScheduleOracleOldVersionDeletion applies only after the Finalize host verifies
// the DEPRECATED old version, all consumer exits and rollback grace. Scheduling
// can make the version immediately unusable. The requested date governs permanent deletion.
func (c Client) ScheduleOracleOldVersionDeletion(ctx context.Context, gate HostAuthorize, region, secretID, etag string, version uint64, deleteAt time.Time) (Receipt, error) {
	return c.scheduleOracleOldVersionDeletion(ctx, gate, region, secretID, etag, version, deleteAt, nil)
}

// ScheduleOracleOldVersionDeletionWithFence checks the supplied fence after
// signing and immediately before sending the exact old-version request.
func (c Client) ScheduleOracleOldVersionDeletionWithFence(ctx context.Context, gate HostAuthorize, region, secretID, etag string, version uint64, deleteAt time.Time, beforeSend func(context.Context) error) (Receipt, error) {
	if beforeSend == nil {
		return Receipt{}, errors.New("OCI deletion final fence required")
	}
	return c.scheduleOracleOldVersionDeletion(ctx, gate, region, secretID, etag, version, deleteAt, beforeSend)
}

func (c Client) scheduleOracleOldVersionDeletion(ctx context.Context, gate HostAuthorize, region, secretID, etag string, version uint64, deleteAt time.Time, beforeSend func(context.Context) error) (Receipt, error) {
	deleteAt = deleteAt.UTC().Truncate(time.Second)
	if !regionID.MatchString(region) || !validOCI(secretID, "vaultsecret") || invalidETag(etag) || version == 0 || version > math.MaxInt64 || !validOracleDeletionTime(deleteAt, time.Now()) {
		return Receipt{}, errors.New("invalid OCI deprecated-version deletion request")
	}
	name := strconv.FormatUint(version, 10)
	target := region + "/" + secretID + "/version/" + name
	if err := authorize(ctx, gate, "oracle", oracleScheduleAction, target); err != nil {
		return Receipt{}, err
	}
	if beforeSend == nil {
		beforeSend = func(ctx context.Context) error {
			return authorize(ctx, gate, "oracle", oracleScheduleAction, target)
		}
	}
	fence := func(ctx context.Context) error {
		if err := beforeSend(ctx); err != nil {
			return err
		}
		if !validOracleDeletionTime(deleteAt, time.Now()) {
			return errors.New("OCI deletion date outside scheduling window")
		}
		return nil
	}
	body, _ := json.Marshal(map[string]string{"timeOfDeletion": deleteAt.Format(time.RFC3339)})
	_, err := c.callOracle(ctx, http.MethodPost, region, "/20180608/secrets/"+secretID+"/version/"+name+"/actions/scheduleDeletion", etag, body, fence, http.StatusNoContent)
	if err != nil {
		return Receipt{}, err
	}
	return receipt("oracle", oracleScheduleAction, secretID, name), nil
}

func validOracleDeletionTime(deleteAt, now time.Time) bool {
	return !deleteAt.Before(now.Add(24*time.Hour)) && !deleteAt.After(now.Add(30*24*time.Hour))
}

func invalidETag(etag string) bool {
	if etag == "" || len(etag) > 128 || etag == "*" || strings.Contains(etag, ",") {
		return true
	}
	if len(etag) >= 2 && etag[0] == '"' && etag[len(etag)-1] == '"' {
		etag = etag[1 : len(etag)-1]
	}
	for _, r := range etag {
		if r <= ' ' || r >= 0x7f || r == '"' {
			return true
		}
	}
	return etag == ""
}

func (c Client) callOracle(ctx context.Context, method, region, path, etag string, body []byte, beforeSend func(context.Context) error, expected int) ([]byte, error) {
	if c.http == nil || c.sign == nil || ctx.Err() != nil {
		return nil, errors.New("OCI mutation client unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://vaults."+region+".oci.oraclecloud.com"+path, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("cannot build OCI mutation")
	}
	req.GetBody = nil
	req.Header.Set("If-Match", etag)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	sum := sha256.Sum256(body)
	req.Header.Set("X-Content-Sha256", base64.StdEncoding.EncodeToString(sum[:]))

	// The signer can read or replace its body. Keep the dispatch request and
	// its bytes private and use the signer's copy only to verify the signature inputs.
	signingBody := bytes.Clone(body)
	defer clear(signingBody)
	signed := req.Clone(ctx)
	signed.Body = io.NopCloser(bytes.NewReader(signingBody))
	if err := c.sign(signed); err != nil {
		return nil, errors.New("OCI signer refused operation")
	}
	if !sameOracleSignedRequest(req, signed, body) {
		return nil, errors.New("OCI signer mutated immutable effect")
	}
	req.Header = signed.Header.Clone()

	if err := beforeSend(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := c.send(req, expected)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOracleEffectUnknown, err)
	}
	return data, nil
}

func sameOracleSignedRequest(req, signed *http.Request, body []byte) bool {
	if signed.Body == nil {
		return false
	}
	// Read and close the signer-controlled body before validating the rest of
	// the request, so those operations cannot change the headers we forward.
	reader := signed.Body
	signedBody, err := io.ReadAll(io.LimitReader(reader, int64(len(body)+1)))
	defer clear(signedBody)
	if closeErr := reader.Close(); err != nil || closeErr != nil || !bytes.Equal(signedBody, body) {
		return false
	}
	if signed.Method != req.Method || signed.URL == nil || *signed.URL != *req.URL ||
		signed.Host != req.Host || signed.ContentLength != req.ContentLength ||
		signed.GetBody != nil || signed.RequestURI != "" ||
		len(signed.TransferEncoding) != 0 || len(signed.Trailer) != 0 || signed.Close {
		return false
	}
	for key, values := range req.Header {
		if !slices.Equal(signed.Header[key], values) {
			return false
		}
	}
	for key, values := range signed.Header {
		if _, fixed := req.Header[key]; fixed {
			continue
		}
		switch key {
		case "Authorization", "Date", "X-Date":
			if len(values) != 1 || strings.TrimSpace(values[0]) != values[0] || values[0] == "" || strings.ContainsAny(values[0], "\r\n\x00") {
				return false
			}
		default:
			return false
		}
	}
	return signed.Header.Get("Authorization") != ""
}
