// Package hostbridge gates KeyCLI provider effects on a trusted host admission
// decision. It does not create authority and it does not talk to any provider.
//
// The package exists because providerlifecycle.HostAuthorize is a callback: its
// mere presence proves nothing. A caller can pass a permissive closure, and a
// model-controlled argument cannot be distinguished from a host decision by
// inspection. This bridge therefore
//
//  1. classifies the requested operation from a fixed table (unknown = refuse),
//  2. builds the admission request from the *actual* arguments, never from
//     model-provided text,
//  3. rejects any decision that is not bound to that exact request,
//  4. refuses decisions that are expired, over-long-lived, or not authorising.
//
// A decision still has to come from a host-owned Admitter. See
// workspace-infra docs/contracts/keycli-host-admission-v1.md for the authoritative
// evaluation side.
package hostbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
)

// Decision status values.
const (
	StatusAdmitted = "ADMITTED"
	StatusBlocked  = "BLOCKED"
)

// Admission actions. They map onto existing workspace-infra canonical effects;
// this package never invents a new effect name.
const (
	ActionObserve = "observe"
	ActionStage   = "stage"
	ActionIssue   = "issue"
	ActionRevoke  = "revoke"
)

var (
	// ErrNoAdmitter means the bridge has no trusted host evaluator.
	ErrNoAdmitter = errors.New("host admission requires a trusted admitter")
	// ErrUnknownOperation means the provider operation is not in the fixed table.
	ErrUnknownOperation = errors.New("provider operation is not classified")
	// ErrAdmissionRejected means the host refused the effect.
	ErrAdmissionRejected = errors.New("host admission rejected the provider effect")
	// ErrAdmissionInvalid means the decision itself is malformed or stale.
	ErrAdmissionInvalid = errors.New("host admission decision is not usable")
	// ErrBindingMismatch means the decision admits a different request.
	ErrBindingMismatch = errors.New("host admission decision is bound to another request")
)

// Identity is the host-owned part of an admission request. Every field must come
// from the trusted host process, never from a model or CLI argument.
type Identity struct {
	GoalID              string
	Repository          string
	HeadSHA             string
	BaseSHA             string
	OperationID         string
	MutationID          string
	WriterGeneration    int64
	WriterOwner         string
	WriterSession       string
	HostIdentity        string
	Paths               []string
	BudgetCost          int64
	CredentialIsolation string
	IsolationDigest     string
	ReconcileOf         string
}

// Request is the normalised admission request sent to the host evaluator.
type Request struct {
	GoalID                    string
	OperationID               string
	MutationID                string
	Repository                string
	HeadSHA                   string
	BaseSHA                   string
	Provider                  string
	Action                    string
	TargetDigest              string
	OperationDigest           string
	WriterGeneration          int64
	WriterOwner               string
	WriterSession             string
	HostIdentity              string
	Paths                     []string
	BudgetCost                int64
	CredentialIsolation       string
	CredentialIsolationDigest string
	ReconcileOf               string
}

// Decision is the host evaluator's answer. Bound must equal the Request that was
// sent; the bridge re-checks every field.
type Decision struct {
	Status              string
	Reason              string
	GrantID             string
	AuthorizesExecution bool
	ProviderMutation    bool
	ExpiresAt           time.Time
	Bound               Request
}

// Admitter is implemented by the trusted host process.
type Admitter interface {
	Admit(req Request) (Decision, error)
}

type operationClass struct {
	action   string
	mutation bool
}

// operationTable is exhaustive on purpose: a provider operation that is not
// listed is refused instead of guessed. The strings are the exact values passed
// to providerlifecycle.HostAuthorize.
var operationTable = map[string]operationClass{
	"cloudflare/create-account-token":                   {ActionIssue, true},
	"cloudflare/stage-inactive-worker-version-secret":   {ActionStage, true},
	"cloudflare/delete-exact-account-token":             {ActionRevoke, true},
	"github/delete-exact-repository-secret":             {ActionRevoke, true},
	"github/stage-repository-generation-secret":         {ActionStage, true},
	"oracle/stage-pending-vault-version":                {ActionStage, true},
	"oracle/schedule-deprecated-vault-version-deletion": {ActionRevoke, true},
	// Synthetic local_test only, never a provider credential operation.
	"synthetic/record-local-test": {ActionStage, true},
}

// ClassifyOperation resolves provider + operation to an admission action.
func ClassifyOperation(provider, operation string) (action string, mutating bool, err error) {
	if provider == "" || operation == "" {
		return "", false, ErrUnknownOperation
	}
	class, ok := operationTable[provider+"/"+operation]
	if !ok {
		return "", false, ErrUnknownOperation
	}
	return class.action, class.mutation, nil
}

// TargetDigest binds provider, operation and provider resource identity. It is
// metadata only and must never be derived from a credential value.
func TargetDigest(provider, operation, target string) string {
	return digestOf(provider, operation, target)
}

// OperationDigest binds the local operation id to its target digest.
func OperationDigest(operationID, targetDigest string) string {
	return digestOf(operationID, targetDigest)
}

func digestOf(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// RequestDigest is a canonical digest of the request fields, used to compare a
// decision's binding without depending on struct field order.
func (r Request) Digest() string {
	paths := append([]string(nil), r.Paths...)
	sort.Strings(paths)
	return digestOf(
		r.GoalID,
		r.OperationID,
		r.MutationID,
		r.Repository,
		r.HeadSHA,
		r.BaseSHA,
		r.Provider,
		r.Action,
		r.TargetDigest,
		r.OperationDigest,
		intString(r.WriterGeneration),
		r.WriterOwner,
		r.WriterSession,
		r.HostIdentity,
		strings.Join(paths, "\x01"),
		intString(r.BudgetCost),
		r.CredentialIsolation,
		r.CredentialIsolationDigest,
		r.ReconcileOf,
	)
}

func intString(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [20]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
