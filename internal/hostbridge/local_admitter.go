package hostbridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	localDeadline = 3 * time.Second
	localMaxFrame = 64 << 10
)

var (
	ErrLocalAuthorityUnavailable = errors.New("AUTHORITY_UNAVAILABLE")
	ErrLocalAuthorityInvalid     = errors.New("AUTHORITY_REPLY_INVALID")
	ErrExecutionFenceUnavailable = errors.New("EXECUTION_FENCE_UNAVAILABLE")
	wireIdentifier               = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	wireOperation                = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	wireDigest                   = regexp.MustCompile(`^[a-f0-9]{64}$`)
	wireTimestamp                = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$`)
)

// AuthorityBinding is the protected registration projection for one exact intent.
// Operation identifies the registration; MutationID identifies the dispatch.
type AuthorityBinding struct {
	GoalID           string `json:"goal_id"`
	MutationID       string `json:"mutation_id"`
	WriterGeneration int64  `json:"writer_generation"`
	Owner            string `json:"owner"`
	Session          string `json:"session"`
	Provider         string `json:"provider"`
	Operation        string `json:"operation"`
	ResourceDigest   string `json:"resource_digest"`
	TargetDigest     string `json:"target_digest"`
	PlanDigest       string `json:"plan_digest"`
	GrantDigest      string `json:"grant_digest"`
}

type authorityRequest struct {
	Version   int    `json:"v"`
	RequestID string `json:"request_id"`
	Method    string `json:"method"`
	AuthorityBinding
}

// Keep the field order aligned with the Python v1 canonical advisory digest.
type authorityAdvisory struct {
	Version   int    `json:"v"`
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Reason    string `json:"reason"`
	AuthorityBinding
	ExpiresAt     *string `json:"expires_at"`
	MutationState *string `json:"mutation_state"`
	Authorizes    bool    `json:"authorizes_execution"`
}

type authorityReply struct {
	authorityAdvisory
	AdmissionDigest *string `json:"admission_digest"`
}

// LocalAdmitter uses one host-configured filesystem Unix stream endpoint for
// its lifetime. Its UID is pinned independently of any writer request.
type LocalAdmitter struct {
	path string
	uid  uint32
}

func NewLocalAdmitter(socketPath string, authorityUID uint32) (*LocalAdmitter, error) {
	if !filepath.IsAbs(socketPath) || filepath.Clean(socketPath) != socketPath {
		return nil, ErrLocalAuthorityUnavailable
	}
	return &LocalAdmitter{path: socketPath, uid: authorityUID}, nil
}

// Preflight checks whether the bound intent may proceed to the local journal
// and final verification. Success is advisory, never an execution permit.
func (a *LocalAdmitter) Preflight(ctx context.Context, binding AuthorityBinding, now func() time.Time) (time.Time, error) {
	reply, err := a.exchange(ctx, "preview_admission", binding)
	if err != nil {
		return time.Time{}, err
	}
	if reply.Status != "ADVISORY" {
		return time.Time{}, ErrAdmissionRejected
	}
	expiry, _ := parseWireTime(reply.ExpiresAt)
	if now == nil {
		now = time.Now
	}
	at := now()
	if !at.Before(expiry) || expiry.After(at.Add(MaxAdmissionLifetime)) {
		return time.Time{}, ErrAdmissionInvalid
	}
	return expiry, nil
}

// VerifyExecution submits the final bound fence query. V1 only returns
// non-authorizing observations, including its synthetic EXECUTION_OBSERVED.
func (a *LocalAdmitter) VerifyExecution(ctx context.Context, binding AuthorityBinding) error {
	reply, err := a.exchange(ctx, "preview_execution", binding)
	if err != nil {
		return err
	}
	if reply.Status == "BLOCKED" && reply.Reason != "EXECUTION_FENCE_UNAVAILABLE" {
		return ErrAdmissionRejected
	}
	return ErrExecutionFenceUnavailable
}

func (a *LocalAdmitter) exchange(ctx context.Context, method string, binding AuthorityBinding) (authorityReply, error) {
	fail := func() (authorityReply, error) { return authorityReply{}, ErrLocalAuthorityUnavailable }
	if a == nil || ctx == nil || ctx.Err() != nil {
		return fail()
	}
	if !binding.Valid() {
		return authorityReply{}, ErrBindingMismatch
	}
	ctx, cancel := context.WithTimeout(ctx, localDeadline)
	defer cancel()
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fail()
	}
	req := authorityRequest{1, hex.EncodeToString(nonce[:]), method, binding}
	payload, err := json.Marshal(req)
	if err != nil {
		return fail()
	}
	info, err := os.Lstat(a.path)
	if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !authoritySocketOwner(info, a.uid) {
		return fail()
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", a.path)
	if err != nil {
		return fail()
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return fail()
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok || !authorityPeerUID(unixConn, a.uid) {
		return fail()
	}
	// The supervisor owns the pathname and service UID. Refuse replacement
	// detected during connect and authenticate the connected kernel peer.
	connectedInfo, err := os.Lstat(a.path)
	if err != nil || !os.SameFile(info, connectedInfo) || connectedInfo.Mode().Perm() != 0600 || !authoritySocketOwner(connectedInfo, a.uid) {
		return fail()
	}
	if n, err := unixConn.Write(payload); err != nil || n != len(payload) || unixConn.CloseWrite() != nil {
		return fail()
	}
	raw, err := io.ReadAll(io.LimitReader(unixConn, localMaxFrame+1))
	if err != nil || len(raw) == 0 || ctx.Err() != nil || !time.Now().Before(deadline) {
		return fail()
	}
	reply, err := validateAuthorityReply(req, raw)
	if ctx.Err() != nil || !time.Now().Before(deadline) {
		return fail()
	}
	return reply, err
}

// Valid reports whether every projected field can be represented in the v1 protocol.
func (b AuthorityBinding) Valid() bool {
	return wireIdentifier.MatchString(b.GoalID) && wireOperation.MatchString(b.MutationID) &&
		wireOperation.MatchString(b.Operation) && b.WriterGeneration > 0 &&
		hostID.MatchString(b.Owner) && wireIdentifier.MatchString(b.Session) &&
		(b.Provider == "cloudflare" || b.Provider == "github" || b.Provider == "oracle") &&
		wireDigest.MatchString(b.ResourceDigest) && wireDigest.MatchString(b.TargetDigest) &&
		wireDigest.MatchString(b.PlanDigest) && wireDigest.MatchString(b.GrantDigest)
}

var replyFields = strings.Fields("v request_id status reason goal_id mutation_id writer_generation owner session provider operation resource_digest target_digest plan_digest grant_digest admission_digest expires_at mutation_state authorizes_execution")

func validateAuthorityReply(req authorityRequest, raw []byte) (authorityReply, error) {
	bad := func() (authorityReply, error) { return authorityReply{}, ErrLocalAuthorityInvalid }
	if len(raw) > localMaxFrame || !utf8.Valid(raw) {
		return bad()
	}
	fields, ok := exactWireObject(raw)
	if !ok {
		return bad()
	}
	if len(fields) != len(replyFields) {
		return bad()
	}
	for _, name := range replyFields {
		if _, ok := fields[name]; !ok {
			return bad()
		}
	}
	var reply authorityReply
	if json.Unmarshal(raw, &reply) != nil || reply.Version != 1 ||
		reply.RequestID != req.RequestID || reply.AuthorityBinding != req.AuthorityBinding ||
		!wireIdentifier.MatchString(reply.Reason) || string(fields["authorizes_execution"]) != "false" {
		return bad()
	}
	switch {
	case (req.Method == "preview_admission" || req.Method == "preview_execution") &&
		reply.Status == "ADVISORY" && reply.MutationState == nil:
		if _, ok := parseWireTime(reply.ExpiresAt); !ok || reply.AdmissionDigest == nil ||
			*reply.AdmissionDigest != jsonDigest(reply.authorityAdvisory) {
			return bad()
		}
	case reply.Status == "BLOCKED" && reply.ExpiresAt == nil && reply.AdmissionDigest == nil && reply.MutationState == nil:
	default:
		return bad()
	}
	return reply, nil
}

func exactWireObject(raw []byte) (map[string]json.RawMessage, bool) {
	d := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}
	fields := make(map[string]json.RawMessage)
	for d.More() {
		tok, err := d.Token()
		key, ok := tok.(string)
		if err != nil || !ok || fields[key] != nil {
			return nil, false
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return nil, false
		}
		fields[key] = value
	}
	if tok, err := d.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}
	_, err := d.Token()
	return fields, err == io.EOF
}

func parseWireTime(v *string) (time.Time, bool) {
	if v == nil || !wireTimestamp.MatchString(*v) {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, *v)
	return t, err == nil
}

func jsonDigest(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
