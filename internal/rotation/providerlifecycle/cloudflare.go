package providerlifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type CloudflarePolicy struct {
	Effect           string            `json:"effect"`
	Resources        map[string]string `json:"resources"`
	PermissionGroups []struct {
		ID string `json:"id"`
	} `json:"permission_groups"`
}

// SecretSink stores the newly issued token only in a host-owned secret store,
// never on disk/logs/CLI stdout. A successful provider Create followed by a
// sink failure is UNKNOWN_EFFECT and MUST be reconciled, not retried.
type SecretSink func(context.Context, []byte) error

// CreateCloudflareAccountToken uses a separately authenticated account-token
// issuer (Account API Tokens Write). The host pins the allowed permission group.
func (c Client) CreateCloudflareAccountToken(ctx context.Context, gate HostAuthorize, account, name, permissionGroupID string, sink SecretSink) (Receipt, error) {
	if !accountID.MatchString(account) || !segment.MatchString(name) || !strings.HasPrefix(name, "kc-rot-") ||
		!accountID.MatchString(permissionGroupID) || sink == nil {
		return Receipt{}, errors.New("invalid narrow Cloudflare issuer scope")
	}
	target := account + "/tokens/" + name + "/permission-group/" + permissionGroupID
	if err := authorize(ctx, gate, "cloudflare", "create-account-token", target); err != nil {
		return Receipt{}, err
	}
	body, _ := json.Marshal(map[string]any{"name": name, "policies": []any{map[string]any{
		"effect": "allow", "resources": map[string]string{"com.cloudflare.api.account." + account: "*"},
		"permission_groups": []any{map[string]string{"id": permissionGroupID}},
	}}})
	data, err := c.call(ctx, http.MethodPost, "https://api.cloudflare.com", "/client/v4/accounts/"+account+"/tokens", body, http.StatusOK, http.StatusCreated)
	if err != nil {
		return Receipt{}, err
	}
	var response struct {
		Success bool `json:"success"`
		Result  struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"result"`
	}
	if json.Unmarshal(data, &response) != nil || !response.Success || !accountID.MatchString(response.Result.ID) || response.Result.Name != name || response.Result.Value == "" {
		return Receipt{}, errors.New("Cloudflare issuer response identity unknown")
	}
	value := []byte(response.Result.Value)
	defer func() {
		for i := range value {
			value[i] = 0
		}
	}()
	if err := sink(ctx, value); err != nil {
		return Receipt{}, errors.New("Cloudflare issued token but storage outcome unknown")
	}
	return receipt("cloudflare", "create-account-token", account, response.Result.ID), nil
}

// DeleteCloudflareAccountToken deletes one verified, previous-generation token
// ID under a separate owner Revoker grant, never by rolling a currently used
// credential. It is ONLY a rotation.Finalize revoke callback.
func (c Client) DeleteCloudflareAccountToken(ctx context.Context, gate HostAuthorize, account, token string) (Receipt, error) {
	if !accountID.MatchString(account) || !accountID.MatchString(token) {
		return Receipt{}, errors.New("invalid Cloudflare token/account ID")
	}
	target := account + "/tokens/" + token
	if err := authorize(ctx, gate, "cloudflare", "delete-exact-account-token", target); err != nil {
		return Receipt{}, err
	}
	data, err := c.call(ctx, http.MethodDelete, "https://api.cloudflare.com", "/client/v4/accounts/"+account+"/tokens/"+token, nil, http.StatusOK)
	if err != nil {
		return Receipt{}, err
	}
	var response struct {
		Success bool `json:"success"`
		Result  struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if json.Unmarshal(data, &response) != nil || !response.Success || response.Result.ID != token {
		return Receipt{}, errors.New("Cloudflare token revocation acknowledgement unknown")
	}
	return receipt("cloudflare", "delete-account-token", account, token), nil
}
