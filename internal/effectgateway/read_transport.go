package effectgateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/camem2442/keycli/internal/authoritybinding"
	"github.com/camem2442/keycli/internal/delivery/journal"
)

const (
	ReadStatus    = "status"
	ReadReconcile = "reconcile"
)

var ErrReadNotFound = errors.New("gateway operation is unavailable")

// ReadRequest authenticates a current session's read of an original mutation.
// The immutable operation binding is loaded from the journal by the Gateway.
type ReadRequest struct {
	GoalID     string    `json:"goal_id"`
	MutationID string    `json:"mutation_id"`
	Owner      string    `json:"owner"`
	Session    string    `json:"session"`
	Generation int64     `json:"generation"`
	ExpiresAt  time.Time `json:"expires_at"`
	Action     string    `json:"action"`
}

type ReadEnvelope struct {
	Request   ReadRequest `json:"request"`
	Signature string      `json:"signature"`
}

type ReadAuthorizer interface {
	AuthorizeRead(context.Context, ReadRequest, authoritybinding.Binding) (RecoveryGrant, error)
}

type recoveryKeyRing interface {
	RecoveryKey(context.Context, string, string, string, int64) ([]byte, error)
	RecoveryKeyForClaim(context.Context, ClaimSnapshot) ([]byte, error)
}

func SignRead(req ReadRequest, key []byte) ReadEnvelope {
	mac := hmac.New(sha256.New, key)
	// A separately prefixed domain prevents submit or status signatures from
	// authorizing any other action. Each component is length-prefixed.
	mac.Write([]byte("keycli/gateway-read/v1\x00"))
	for _, field := range []string{req.Action, req.GoalID, req.MutationID, req.Owner, req.Session,
		strconv.FormatInt(req.Generation, 10), req.ExpiresAt.UTC().Format(time.RFC3339Nano)} {
		mac.Write([]byte(strconv.Itoa(len(field)) + ":" + field))
	}
	return ReadEnvelope{Request: req, Signature: hex.EncodeToString(mac.Sum(nil))}
}

func (g *Gateway) authenticateRead(ctx context.Context, env ReadEnvelope) (ClaimSnapshot, error) {
	if g == nil || ctx == nil || ctx.Err() != nil {
		return ClaimSnapshot{}, ErrAuthorityUnavail
	}
	req := env.Request
	if req.Action != ReadStatus && req.Action != ReadReconcile || !journalSafeID(req.GoalID) ||
		!journal.ValidOperationID(req.MutationID) || req.Owner == "" || len(req.Owner) > 200 ||
		req.Session == "" || len(req.Session) > 200 || req.Generation < 1 || req.ExpiresAt.IsZero() {
		return ClaimSnapshot{}, ErrInvalidRequest
	}
	now := g.now()
	if !now.Before(req.ExpiresAt) {
		return ClaimSnapshot{}, ErrExpired
	}
	if req.ExpiresAt.After(now.Add(maxRequestTTL)) {
		return ClaimSnapshot{}, ErrInvalidRequest
	}
	ring, ok := g.Keys.(recoveryKeyRing)
	if !ok || g.Claims == nil || g.ReadAuthority == nil || g.Journal.Root == "" {
		return ClaimSnapshot{}, ErrNotConfigured
	}
	key, err := ring.RecoveryKey(ctx, req.GoalID, req.Owner, req.Session, req.Generation)
	if err != nil || len(key) < minKeyBytes {
		return ClaimSnapshot{}, ErrUnauthenticated
	}
	defer clear(key)
	want, err := hex.DecodeString(env.Signature)
	expected, _ := hex.DecodeString(SignRead(req, key).Signature)
	if err != nil || !hmac.Equal(want, expected) {
		return ClaimSnapshot{}, ErrUnauthenticated
	}
	claim, err := g.Claims.Snapshot(ctx, req.GoalID)
	if err != nil {
		return ClaimSnapshot{}, ErrAuthorityUnavail
	}
	identity := Request{GoalID: req.GoalID, Owner: req.Owner, Session: req.Session, Generation: req.Generation}
	state := claim.State
	if state == "RECONCILING" {
		claim.State = "ACTIVE"
	}
	if err := checkClaim(claim, identity, g.now()); err != nil {
		return ClaimSnapshot{}, err
	}
	claim.State = state
	stateKey, err := ring.RecoveryKeyForClaim(ctx, claim)
	defer clear(stateKey)
	if err != nil || !hmac.Equal(key, stateKey) {
		return ClaimSnapshot{}, ErrUnauthenticated
	}
	return claim, nil
}

func (g *Gateway) authorizeRead(ctx context.Context, env ReadEnvelope, binding authoritybinding.Binding) (RecoveryGrant, error) {
	req := env.Request
	claim, err := g.authenticateRead(ctx, env)
	if err != nil {
		return RecoveryGrant{}, err
	}
	if !binding.Valid() || binding.GoalID != req.GoalID || binding.MutationID != req.MutationID ||
		claim.Generation < binding.WriterGeneration ||
		(claim.Generation == binding.WriterGeneration && (claim.Owner != binding.Owner || claim.Session != binding.Session || claim.State == "RECONCILING")) {
		return RecoveryGrant{}, ErrReadNotFound
	}
	grant, err := g.ReadAuthority.AuthorizeRead(ctx, req, binding)
	if err != nil || ctx.Err() != nil || !validRecoveryGrant(grant, binding, g.now()) {
		return RecoveryGrant{}, ErrAdmissionRejected
	}
	if grant.ExpiresAt.After(req.ExpiresAt) {
		grant.ExpiresAt = req.ExpiresAt
	}
	if !claim.LeaseExpiresAt.IsZero() && grant.ExpiresAt.After(claim.LeaseExpiresAt) {
		grant.ExpiresAt = claim.LeaseExpiresAt
	}
	if !g.now().Before(grant.ExpiresAt) {
		return RecoveryGrant{}, ErrExpired
	}
	return grant, nil
}

func (g *Gateway) Read(ctx context.Context, env ReadEnvelope) (Receipt, error) {
	if _, err := g.authenticateRead(ctx, env); err != nil {
		return Receipt{}, err
	}
	record, err := g.Journal.Status(env.Request.MutationID)
	if errors.Is(err, journal.ErrNotFound) || (err == nil && record.Binding == nil) {
		return Receipt{}, ErrReadNotFound
	}
	if err != nil {
		return Receipt{}, ErrJournalUnavailable
	}
	binding := *record.Binding
	if _, err := g.authorizeRead(ctx, env, binding); err != nil {
		return Receipt{}, err
	}
	if env.Request.Action == ReadReconcile {
		var authorizationErr error
		record, err = g.reconcile(ctx, env.Request.MutationID, func(ctx context.Context, original authoritybinding.Binding) (RecoveryGrant, error) {
			if original != binding {
				authorizationErr = ErrReadNotFound
				return RecoveryGrant{}, authorizationErr
			}
			grant, authErr := g.authorizeRead(ctx, env, original)
			authorizationErr = authErr
			return grant, authErr
		}, func(ctx context.Context) error {
			_, authorizationErr = g.authenticateRead(ctx, env)
			return authorizationErr
		})
		if authorizationErr != nil {
			return Receipt{}, authorizationErr
		}
		if errors.Is(err, journal.ErrBusy) {
			return Receipt{}, ErrGatewayBusy
		}
		if err != nil && !errors.Is(err, journal.ErrUnknownEffect) {
			return Receipt{}, ErrJournalUnavailable
		}
		if _, err := g.authorizeRead(ctx, env, binding); err != nil {
			return Receipt{}, err
		}
	}
	return readReceipt(record), nil
}

func readReceipt(record journal.Record) Receipt {
	status := StatusUnknown
	switch record.State {
	case journal.Acknowledged, journal.ObservedApplied:
		status = StatusApplied
	case journal.ObservedNoEffect:
		status = StatusNoEffect
	}
	return Receipt{MutationID: record.OperationID, Status: status, OperationState: string(record.State), ProofDigest: record.ProofDigest}
}
