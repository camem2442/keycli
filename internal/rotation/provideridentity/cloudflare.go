package provideridentity

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/rotation"
)

// CloudflareAccountToken requires an independent Account API Tokens Read grant
// (not the token being revoked). It never accepts a Worker Secret ID or token
// VALUE as identity. IssuedOn and policy digest must be pinned by the issuer.
type CloudflareAccountToken struct {
	Reader                                 Reader
	PlanTarget                             string
	AccountID, TokenID, ExpectedName       string
	ExpectedIssuedOn, ExpectedPolicyDigest string
}

func (o CloudflareAccountToken) Verify(ctx context.Context, p rotation.PreparationPlan, handle string) (rotation.VerifiedOldCredential, error) {
	invalid := errors.New("Cloudflare account token identity unavailable or changed")
	if p.Old.Provider != "cloudflare" || p.Old.Kind != rotation.StaticServiceKey || !validPlan(p) || p.Old.Target != o.PlanTarget ||
		o.TokenID != handle || !hex32(o.AccountID) || !hex32(handle) || !noWildcard(o.ExpectedName) ||
		!eqTime(o.ExpectedIssuedOn, o.ExpectedIssuedOn) || len(o.ExpectedPolicyDigest) != 64 {
		return rotation.VerifiedOldCredential{}, invalid
	}
	var account struct {
		Success bool `json:"success"`
		Result  struct {
			ID string `json:"id"`
		} `json:"result"`
	}
	if err := o.Reader.read(ctx, "https://api.cloudflare.com", "/client/v4/accounts/"+o.AccountID, &account, nil); err != nil || !account.Success || !strings.EqualFold(account.Result.ID, o.AccountID) {
		return rotation.VerifiedOldCredential{}, invalid
	}
	var token struct {
		Success bool `json:"success"`
		Result  struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Status    string          `json:"status"`
			IssuedOn  string          `json:"issued_on"`
			Policies  json.RawMessage `json:"policies"`
			ExpiresOn string          `json:"expires_on"`
			NotBefore string          `json:"not_before"`
		} `json:"result"`
	}
	if err := o.Reader.read(ctx, "https://api.cloudflare.com", "/client/v4/accounts/"+o.AccountID+"/tokens/"+handle, &token, nil); err != nil || !token.Success {
		return rotation.VerifiedOldCredential{}, invalid
	}
	x := token.Result
	if !strings.EqualFold(x.ID, handle) || x.Name != o.ExpectedName || x.Status != "active" || !eqTime(x.IssuedOn, o.ExpectedIssuedOn) || len(x.Policies) == 0 || string(x.Policies) == "null" {
		return rotation.VerifiedOldCredential{}, invalid
	}
	if x.ExpiresOn != "" {
		ex, e := fixedTime(x.ExpiresOn)
		if e != nil || !ex.After(time.Now()) {
			return rotation.VerifiedOldCredential{}, invalid
		}
	}
	if x.NotBefore != "" {
		nb, e := fixedTime(x.NotBefore)
		if e != nil || nb.After(time.Now()) {
			return rotation.VerifiedOldCredential{}, invalid
		}
	}
	var policies []struct {
		Effect    string                     `json:"effect"`
		Resources map[string]json.RawMessage `json:"resources"`
	}
	if json.Unmarshal(x.Policies, &policies) != nil || len(policies) == 0 || len(policies) > 32 {
		return rotation.VerifiedOldCredential{}, invalid
	}
	expectedKey := "com.cloudflare.api.account." + o.AccountID
	scoped := false
	for _, policy := range policies {
		if policy.Effect != "allow" && policy.Effect != "deny" {
			return rotation.VerifiedOldCredential{}, invalid
		}
		for k := range policy.Resources {
			if strings.Contains(k, ".account.*") || strings.Contains(k, ".zone.*") || !noWildcard(k) {
				return rotation.VerifiedOldCredential{}, invalid
			}
			if k == expectedKey && policy.Effect == "allow" {
				scoped = true
			}
		}
	}
	if !scoped {
		return rotation.VerifiedOldCredential{}, invalid
	}
	var canonical any
	if json.Unmarshal(x.Policies, &canonical) != nil {
		return rotation.VerifiedOldCredential{}, invalid
	}
	policiesDigest := shaMetadata(canonical)
	if policiesDigest != o.ExpectedPolicyDigest {
		return rotation.VerifiedOldCredential{}, invalid
	}
	return rotation.VerifiedOldCredential{Provider: "cloudflare", Kind: "cloudflare-account-token", PlanDigest: p.ID, PlanTarget: p.Old.Target,
		AccountID: o.AccountID, ResourceID: o.AccountID, CredentialID: strings.ToLower(handle), Generation: p.Old.Number, ObservedAt: time.Now().UTC(),
		ProofDigest: shaMetadata(struct{ Account, Token, Name, Issued, Policy, Expiry, NotBefore string }{o.AccountID, x.ID, x.Name, x.IssuedOn, policiesDigest, x.ExpiresOn, x.NotBefore})}, nil
}
func hex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// VerifyWorkersRuntimeGroup checks a host-pinned permission group against the
// account inventory using a read credential independent of the issuer.
func (r Reader) VerifyWorkersRuntimeGroup(ctx context.Context, account, group string) error {
	invalid := errors.New("Cloudflare Workers runtime permission group unavailable or changed")
	if !hex32(account) || !hex32(group) {
		return invalid
	}
	var inventory struct {
		Success bool `json:"success"`
		Result  []struct {
			ID         string   `json:"id"`
			Name       string   `json:"name"`
			Scopes     []string `json:"scopes"`
			Selectable bool     `json:"is_selectable"`
		} `json:"result"`
	}
	if err := r.read(ctx, "https://api.cloudflare.com", "/client/v4/accounts/"+account+"/tokens/permission_groups", &inventory, nil); err != nil || !inventory.Success {
		return invalid
	}
	matches := 0
	for _, entry := range inventory.Result {
		if entry.ID != group {
			continue
		}
		matches++
		if !entry.Selectable || len(entry.Scopes) != 1 || entry.Scopes[0] != "com.cloudflare.api.account" {
			return invalid
		}
		switch entry.Name {
		case "Workers Scripts Read", "Workers Scripts Write", "Workers Scripts Edit":
		default:
			return invalid
		}
	}
	if matches != 1 {
		return invalid
	}
	return nil
}

// VerifyIssuedWorkersToken checks the saved token identity, expiration and
// single-account policy through the independent read client.
func (r Reader) VerifyIssuedWorkersToken(ctx context.Context, account, token, name, group string, expiresAt time.Time) error {
	invalid := errors.New("Cloudflare issued token identity unavailable or changed")
	if !hex32(account) || !hex32(token) || !hex32(group) || !noWildcard(name) || expiresAt.IsZero() {
		return invalid
	}
	var observed struct {
		Success bool `json:"success"`
		Result  struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Status    string `json:"status"`
			ExpiresOn string `json:"expires_on"`
			Policies  []struct {
				Effect    string            `json:"effect"`
				Resources map[string]string `json:"resources"`
				Groups    []struct {
					ID string `json:"id"`
				} `json:"permission_groups"`
			} `json:"policies"`
		} `json:"result"`
	}
	if err := r.read(ctx, "https://api.cloudflare.com", "/client/v4/accounts/"+account+"/tokens/"+token, &observed, nil); err != nil || !observed.Success {
		return invalid
	}
	got := observed.Result
	expiry, err := time.Parse(time.RFC3339, got.ExpiresOn)
	if err != nil || !expiry.Equal(expiresAt.UTC().Truncate(time.Second)) ||
		got.ID != token || got.Name != name || got.Status != "active" || len(got.Policies) != 1 {
		return invalid
	}
	policy := got.Policies[0]
	if policy.Effect != "allow" || len(policy.Resources) != 1 || policy.Resources["com.cloudflare.api.account."+account] != "*" ||
		len(policy.Groups) != 1 || policy.Groups[0].ID != group {
		return invalid
	}
	return nil
}
