// Package freegate provides a fail-closed, read-only Worker Free pilot preflight.
// It NEVER authorizes a Cloudflare write: a separately trusted Goal/Writer host
// must recheck identity, cost and operation authority immediately before effect.
package freegate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
)

const MaxFreeWorkers = 100 // public Free envelope, not proof of account plan
const MaxFreeDailyRequests = 100000

var accountPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var pilotPattern = regexp.MustCompile(`^kc-v09-stage-pilot-[a-z0-9][a-z0-9-]{0,26}$`)

// Evidence is an OWNER-signed observation of a Free dashboard and provider
// inventory. A signature proves authorship, not the provider billing ledger.
// It does not contain tokens, personal billing details or secret values.
type Evidence struct {
	Version             int      `json:"v"`
	AccountID           string   `json:"account_id"`
	WorkerName          string   `json:"worker_name"`
	Operation           string   `json:"operation"` // bootstrap, stage, cleanup
	Plan                string   `json:"plan"`      // exactly workers-free
	Origin              string   `json:"origin"`    // exactly owner-dashboard-attestation
	ObservedUnix        int64    `json:"observed_unix"`
	ExpiresUnix         int64    `json:"expires_unix"`
	Workers             []string `json:"workers"` // complete exact provider GET list
	NoPaidBindings      bool     `json:"no_paid_bindings"`
	NoRoutesOrTriggers  bool     `json:"no_routes_or_triggers"`
	WorkersDevDisabled  bool     `json:"workers_dev_disabled"`
	BudgetExtraUSDCents int64    `json:"budget_extra_usd_cents"`
	MaxBootstrap        int      `json:"max_bootstrap"`
	MaxStage            int      `json:"max_stage"`
	MaxCleanup          int      `json:"max_cleanup"`
	MaxGET              int      `json:"max_get"`
}

type SignedEvidence struct {
	Evidence  Evidence `json:"evidence"`
	Signature []byte   `json:"signature"`
}

// ExpectedScope identifies the account, Worker and operation selected by the host.
type ExpectedScope struct {
	AccountID  string
	WorkerName string
	Operation  string
}

type InventoryReader func(context.Context, string) ([]string, error)

type Decision struct {
	Status              string `json:"status"`
	Reason              string `json:"reason"`
	AccountID           string `json:"account_id"`
	Pilot               string `json:"pilot"`
	Operation           string `json:"operation"`
	ProofDigest         string `json:"proof_digest"`
	Authoritative       bool   `json:"authoritative"`
	AuthorizesExecution bool   `json:"authorizes_execution"`
}

func signatureMessage(e Evidence) []byte {
	body, _ := json.Marshal(e)
	return append([]byte("keycli-cf-freegate-v1\x00"), body...)
}

// Admit checks signed evidence against the host-selected scope before observing
// the inventory. The operating host must pin the owner key independently of the
// evidence and supply the expected scope from the intended operation.
func Admit(ctx context.Context, now time.Time, expected ExpectedScope, ownerKey ed25519.PublicKey, signed SignedEvidence, getWorkers InventoryReader) (Decision, error) {
	blocked := Decision{Status: "BLOCKED", Reason: "Free/account/host evidence unavailable", Authoritative: false, AuthorizesExecution: false}
	e := signed.Evidence
	if ctx.Err() != nil || len(ownerKey) != ed25519.PublicKeySize || getWorkers == nil ||
		!ed25519.Verify(ownerKey, signatureMessage(e), signed.Signature) ||
		e.Version != 1 || !accountPattern.MatchString(e.AccountID) || !pilotPattern.MatchString(e.WorkerName) ||
		e.Plan != "workers-free" || e.Origin != "owner-dashboard-attestation" ||
		(e.Operation != "bootstrap" && e.Operation != "stage" && e.Operation != "cleanup") ||
		e.ObservedUnix <= 0 || e.ExpiresUnix <= e.ObservedUnix || e.ExpiresUnix-e.ObservedUnix > 24*3600 ||
		now.Unix() < e.ObservedUnix-120 || now.Unix() >= e.ExpiresUnix ||
		!e.NoPaidBindings || !e.NoRoutesOrTriggers || !e.WorkersDevDisabled ||
		e.BudgetExtraUSDCents != 0 || e.MaxBootstrap != 1 || e.MaxStage != 1 ||
		e.MaxCleanup != 1 || e.MaxGET < 1 || e.MaxGET > 32 ||
		len(e.Workers) > MaxFreeWorkers || !canonicalWorkers(e.Workers) {
		return blocked, errors.New("Cloudflare Free pilot is not admitted")
	}
	if e.AccountID != expected.AccountID || e.WorkerName != expected.WorkerName || e.Operation != expected.Operation {
		return blocked, errors.New("Cloudflare Free pilot scope does not match")
	}
	observed, err := getWorkers(ctx, expected.AccountID)
	if err != nil || ctx.Err() != nil || !canonicalWorkers(observed) || !slices.Equal(observed, e.Workers) {
		return blocked, errors.New("Cloudflare provider inventory unavailable or changed")
	}
	exists := slices.Contains(observed, e.WorkerName)
	if (e.Operation == "bootstrap" && (exists || len(observed) >= MaxFreeWorkers)) ||
		(e.Operation != "bootstrap" && !exists) {
		return blocked, errors.New("Cloudflare pilot target is not in the expected state")
	}
	sum := sha256.Sum256(signatureMessage(e))
	return Decision{Status: "OWNER_ATTESTED_SCOPED", Reason: "Free dashboard asserted by pinned owner; separate live effect grant required", AccountID: e.AccountID, Pilot: e.WorkerName, Operation: e.Operation, ProofDigest: hex.EncodeToString(sum[:]), Authoritative: false, AuthorizesExecution: false}, nil
}

func canonicalWorkers(xs []string) bool {
	if xs == nil || len(xs) > MaxFreeWorkers {
		return false
	}
	for i, x := range xs {
		if x == "" || len(x) > 63 || strings.ContainsAny(x, "/\\?*[]{} \n\r\t") {
			return false
		}
		if i > 0 && xs[i-1] >= x {
			return false
		}
	}
	return true
}
