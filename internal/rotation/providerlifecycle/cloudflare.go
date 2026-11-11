package providerlifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

type CloudflarePolicy struct {
	Effect           string            `json:"effect"`
	Resources        map[string]string `json:"resources"`
	PermissionGroups []struct {
		ID string `json:"id"`
	} `json:"permission_groups"`
}

// SecretSink stores the issued value and its token ID under an operation ID.
// Lookup reports only the token ID from a committed store record.
type SecretSink interface {
	Store(ctx context.Context, operationID, tokenID string, value []byte) error
	Lookup(ctx context.Context, operationID string) (tokenID string, found bool, err error)
}

// CreateCloudflareAccountToken preserves the original host callback API.
func (c Client) CreateCloudflareAccountToken(ctx context.Context, gate HostAuthorize, account, name, permissionGroupID string, expiresAt time.Time, sink func(context.Context, []byte) error) (Receipt, error) {
	if sink == nil {
		return Receipt{}, errors.New("Cloudflare token storage required")
	}
	return c.createCloudflareAccountToken(ctx, gate, account, name, permissionGroupID, expiresAt, func(ctx context.Context, _ string, value []byte) error {
		return sink(ctx, value)
	})
}

// IssueCloudflareAccountToken binds storage to the journal operation and the
// provider token ID. On uncertainty callers reconcile via Lookup before acting.
func (c Client) IssueCloudflareAccountToken(ctx context.Context, gate HostAuthorize, account, name, permissionGroupID string, expiresAt time.Time, operationID string, sink SecretSink) (Receipt, error) {
	if sink == nil || operationID == "" {
		return Receipt{}, errors.New("Cloudflare token storage binding required")
	}
	return c.createCloudflareAccountToken(ctx, gate, account, name, permissionGroupID, expiresAt, func(ctx context.Context, tokenID string, value []byte) error {
		return sink.Store(ctx, operationID, tokenID, value)
	})
}

func validCloudflareTokenExpiry(expiresAt, now time.Time) bool {
	return !expiresAt.Before(now.Add(5*time.Minute)) && !expiresAt.After(now.Add(24*time.Hour))
}

func (c Client) createCloudflareAccountToken(ctx context.Context, gate HostAuthorize, account, name, permissionGroupID string, expiresAt time.Time, store func(context.Context, string, []byte) error) (Receipt, error) {
	expiresAt = expiresAt.UTC().Truncate(time.Second)
	if !accountID.MatchString(account) || !segment.MatchString(name) || !strings.HasPrefix(name, "kc-rot-") ||
		!accountID.MatchString(permissionGroupID) || !validCloudflareTokenExpiry(expiresAt, time.Now()) {
		return Receipt{}, errors.New("invalid narrow Cloudflare issuer scope")
	}
	target := account + "/tokens/" + name + "/permission-group/" + permissionGroupID
	body, _ := json.Marshal(map[string]any{"name": name, "expires_on": expiresAt.UTC().Format(time.RFC3339), "policies": []any{map[string]any{
		"effect": "allow", "resources": map[string]string{"com.cloudflare.api.account." + account: "*"},
		"permission_groups": []any{map[string]string{"id": permissionGroupID}},
	}}})
	beforeSend := func(ctx context.Context) error {
		if !validCloudflareTokenExpiry(expiresAt, time.Now()) {
			return errors.New("Cloudflare token expiration outside issuance window")
		}
		if err := authorize(ctx, gate, "cloudflare", "create-account-token", target); err != nil {
			return err
		}
		if !validCloudflareTokenExpiry(expiresAt, time.Now()) {
			return errors.New("Cloudflare token expiration outside issuance window")
		}
		return nil
	}
	data, err := c.callBeforeSend(ctx, http.MethodPost, "https://api.cloudflare.com", "/client/v4/accounts/"+account+"/tokens", body, nil, beforeSend, http.StatusOK, http.StatusCreated)
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
	if err := store(ctx, response.Result.ID, value); err != nil {
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
