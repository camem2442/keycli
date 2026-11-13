package providers

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/effectgateway"
	"github.com/camem2442/keycli/internal/rotation"
	"github.com/camem2442/keycli/internal/rotation/provideridentity"
	"github.com/camem2442/keycli/internal/rotation/providerlifecycle"
)

var (
	errProductConfig = errors.New("invalid or unsafe operational provider configuration")
	bearerToken      = regexp.MustCompile(`^[A-Za-z0-9_-]{1,512}$`)
	issueName        = regexp.MustCompile(`^kc-rot-[A-Za-z0-9_.-]{1,93}$`)
)

type productOperation struct {
	binding  authoritybinding.Binding
	resolve  func(context.Context, effectgateway.Request) (effectgateway.RotationAction, error)
	recovery effectgateway.RecoveryObserver
}

type operationKey struct{ goal, mutation string }

// Product dispatches the complete protected registry to typed provider
// adapters. A configuration with unsupported registrations fails startup.
type Product struct {
	operations map[operationKey]productOperation
}

func LoadProduct(path string, authority effectgateway.LocalAuthority) (Product, error) {
	if err := effectgateway.AllowRealProvider("kc-gateway"); err != nil {
		return Product{}, err
	}
	var version int
	var issuerFile, readerFile, storePath string
	var operations []json.RawMessage
	err := effectgateway.ReadPrivateConfig(path, map[string]any{
		"v": &version, "issuer_file": &issuerFile, "reader_file": &readerFile,
		"secret_store": &storePath, "operations": &operations,
	})
	registrations := authority.RegisteredOperations()
	if err != nil || version != 1 || len(operations) == 0 || len(operations) != len(registrations) ||
		authority.Admitter == nil || authority.Execution == nil {
		return Product{}, errProductConfig
	}
	issuerToken, err := loadBearer(issuerFile)
	if err != nil {
		return Product{}, errProductConfig
	}
	readerToken, err := loadBearer(readerFile)
	if err != nil || subtle.ConstantTimeCompare([]byte(issuerToken), []byte(readerToken)) == 1 {
		return Product{}, errProductConfig
	}
	issuer, err := providerlifecycle.NewClient(authenticateBearer(issuerToken), nil)
	if err != nil {
		return Product{}, errProductConfig
	}
	issuerPermissions, err := provideridentity.NewReader(authenticateBearer(issuerToken), nil)
	if err != nil {
		return Product{}, errProductConfig
	}
	reader, err := provideridentity.NewReader(authenticateBearer(readerToken), nil)
	if err != nil {
		return Product{}, errProductConfig
	}
	sink, err := effectgateway.OpenPrivateIssueStore(storePath)
	if err != nil {
		return Product{}, errProductConfig
	}
	product := Product{operations: make(map[operationKey]productOperation, len(operations))}
	seenMutations := make(map[string]bool, len(operations))
	for _, raw := range operations {
		var identity struct {
			Goal     string `json:"goal_id"`
			Mutation string `json:"mutation_id"`
		}
		if json.Unmarshal(raw, &identity) != nil {
			return Product{}, errProductConfig
		}
		registration, err := authority.Registry.Resolve(context.Background(), identity.Goal, identity.Mutation)
		if err != nil {
			return Product{}, errProductConfig
		}
		var entry productOperation
		switch registration.ProviderOperation {
		case cloudflareIssueOperation:
			entry, err = loadIssue(raw, authority, issuer, issuerPermissions, reader, sink)
		case cloudflareDeleteOperation:
			entry, err = loadRetirement(raw, registration, reader, issuerToken, readerToken)
		default:
			return Product{}, errProductConfig
		}
		if err != nil {
			return Product{}, errProductConfig
		}
		binding := entry.binding
		key := operationKey{binding.GoalID, binding.MutationID}
		if seenMutations[binding.MutationID] {
			return Product{}, errProductConfig
		}
		seenMutations[binding.MutationID] = true
		product.operations[key] = entry
	}
	return product, nil
}

func loadIssue(raw json.RawMessage, authority effectgateway.LocalAuthority, issuer providerlifecycle.Client,
	issuerPermissions, reader provideridentity.Reader, sink effectgateway.PrivateIssueStore) (productOperation, error) {
	var goal, mutation, account, name, group, alias string
	var tokenExpiry, grantExpiry time.Time
	var old, next uint64
	var consumerIDs []string
	if !effectgateway.DecodePrivateConfig(raw, map[string]any{
		"goal_id": &goal, "mutation_id": &mutation, "account_id": &account, "token_name": &name,
		"permission_group_id": &group, "token_expires_at": &tokenExpiry, "grant_expires_at": &grantExpiry,
		"credential_alias": &alias, "old_generation": &old, "new_generation": &next, "required_consumers": &consumerIDs,
	}) || !cloudflareID(account) || !cloudflareID(group) || !issueName.MatchString(name) ||
		tokenExpiry.IsZero() || tokenExpiry.Nanosecond() != 0 || grantExpiry.IsZero() || !grantExpiry.Before(tokenExpiry) {
		return productOperation{}, errProductConfig
	}
	reg, err := authority.Registry.Resolve(context.Background(), goal, mutation)
	canonicalTarget := account + "/tokens/" + name + "/permission-group/" + group
	if err != nil || reg.Provider != "cloudflare" || reg.ProviderOperation != cloudflareIssueOperation || reg.CanonicalTarget != canonicalTarget {
		return productOperation{}, errProductConfig
	}
	binding := reg.Binding()
	if !binding.Valid() {
		return productOperation{}, errProductConfig
	}
	consumers := make([]rotation.Consumer, 0, len(consumerIDs))
	for _, id := range consumerIDs {
		consumers = append(consumers, rotation.Consumer{ID: id, Required: true})
	}
	oldGeneration := rotation.Generation{Alias: alias, Kind: rotation.StaticServiceKey, Provider: "cloudflare", Target: account, Number: old}
	newGeneration := oldGeneration
	newGeneration.Number = next
	plan, err := rotation.NewPreparationPlan(oldGeneration, newGeneration, consumers)
	if err != nil || plan.ID != binding.PlanDigest {
		return productOperation{}, errProductConfig
	}
	if _, _, err := sink.Lookup(context.Background(), mutation); err != nil && !errors.Is(err, effectgateway.ErrIssueStoreUncertain) {
		return productOperation{}, errProductConfig
	}
	issue := CloudflareTokenIssue{
		MutationID: mutation, Name: name, PermissionGroupID: group, ExpiresAt: tokenExpiry,
		Issuer: issuer, IssuerPermissions: issuerPermissions, PermissionReader: reader, Sink: sink,
		Authorize: func(ctx context.Context, id string, plan rotation.PreparationPlan) (rotation.PreparationGrant, error) {
			if ctx.Err() != nil || id != binding.MutationID || plan.ID != binding.PlanDigest || !time.Now().Before(grantExpiry) {
				return rotation.PreparationGrant{}, effectgateway.ErrAdmissionRejected
			}
			return rotation.PreparationGrant{PlanDigest: binding.PlanDigest, GrantDigest: binding.GrantDigest, ExpiresAt: grantExpiry}, nil
		},
	}
	route := CloudflareRotation{Plan: plan, AccountID: account, RuntimePermissionGroups: []string{group}, Issue: &issue}
	return productOperation{
		binding:  binding,
		resolve:  route.ResolveRotation,
		recovery: CloudflareIssueRecovery{Binding: binding, AccountID: account, Issue: issue},
	}, nil
}

func (p Product) ResolveRotation(ctx context.Context, request effectgateway.Request) (effectgateway.RotationAction, error) {
	op, ok := p.operations[operationKey{request.GoalID, request.MutationID}]
	if !ok {
		return nil, effectgateway.ErrAdmissionRejected
	}
	if err := effectgateway.AllowRealProvider("kc-gateway"); err != nil {
		return nil, err
	}
	return op.resolve(ctx, request)
}

func (p Product) ObserveRecovery(ctx context.Context, binding authoritybinding.Binding) (effectgateway.RecoveryEvidence, error) {
	op, ok := p.operations[operationKey{binding.GoalID, binding.MutationID}]
	if !ok || op.recovery == nil {
		return effectgateway.RecoveryEvidence{}, effectgateway.ErrAdmissionRejected
	}
	if err := effectgateway.AllowRealProvider("kc-gateway"); err != nil {
		return effectgateway.RecoveryEvidence{}, err
	}
	return op.recovery.ObserveRecovery(ctx, binding)
}

func loadBearer(path string) (string, error) {
	raw, err := effectgateway.ReadPrivateFile(path, 513)
	defer clear(raw)
	if err != nil {
		return "", errProductConfig
	}
	token := strings.TrimSuffix(string(raw), "\n")
	if !bearerToken.MatchString(token) {
		return "", errProductConfig
	}
	return token, nil
}

func authenticateBearer(token string) func(*http.Request) error {
	return func(req *http.Request) error {
		if err := effectgateway.AllowRealProvider("kc-gateway"); err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}
