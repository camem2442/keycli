//go:build darwin || linux

package hostbridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const guardedExecutionMethod = "verify_guarded_execution"

type guardedExecutionRequest struct {
	Version   int    `json:"v"`
	Method    string `json:"method"`
	RequestID string `json:"request_id"`
	AuthorityBinding
	ExpiresAt        string `json:"expires_at"`
	LockOffsetDigest string `json:"lock_offset_digest,omitempty"`
}

var guardedExecutionFields = strings.Fields("v method request_id goal_id mutation_id writer_generation owner session provider operation resource_digest target_digest plan_digest grant_digest expires_at")

func (a *GuardedExecution) exchange(ctx context.Context, binding AuthorityBinding, expiry time.Time, now func() time.Time, lock *os.File) error {
	ctx, cancel := context.WithTimeout(ctx, min(localDeadline, expiry.Sub(now())))
	defer cancel()

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return ErrLocalAuthorityUnavailable
	}
	req := guardedExecutionRequest{
		Version: 2, Method: guardedExecutionMethod, RequestID: hex.EncodeToString(nonce[:]),
		AuthorityBinding: binding, ExpiresAt: expiry.UTC().Format(time.RFC3339Nano),
	}
	info, err := os.Lstat(a.path)
	if err != nil || info.Mode() != os.ModeSocket|0600 || !authoritySocketOwner(info, a.uid) {
		return ErrLocalAuthorityUnavailable
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", a.path)
	if err != nil {
		return ErrLocalAuthorityUnavailable
	}
	unixConn := conn.(*net.UnixConn)
	defer unixConn.Close()
	deadline, _ := ctx.Deadline()
	if err := unixConn.SetDeadline(deadline); err != nil {
		return ErrLocalAuthorityUnavailable
	}
	stop := context.AfterFunc(ctx, func() { _ = unixConn.Close() })
	defer stop()
	connected, err := os.Lstat(a.path)
	if err != nil || connected.Mode() != os.ModeSocket|0600 || !os.SameFile(info, connected) ||
		!authoritySocketOwner(connected, a.uid) || !authorityPeerProcess(unixConn, a.uid, a.pid) {
		return ErrLocalAuthorityUnavailable
	}
	if runtime.GOOS == "darwin" {
		req.Version = 3
		req.LockOffsetDigest, err = proveGuardedLockOffset(lock)
		if err != nil {
			return ErrLocalAuthorityUnavailable
		}
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return ErrLocalAuthorityUnavailable
	}
	fresh := func() bool {
		return ctx.Err() == nil && time.Now().Before(deadline) && now().Before(expiry)
	}
	if !fresh() {
		return ErrLocalAuthorityUnavailable
	}

	rights := unix.UnixRights(int(lock.Fd()))
	// From the first send attempt the receiver may have obtained both the
	// request and the descriptor, even if the local operation returns an error.
	n, oobn, err := unixConn.WriteMsgUnix(payload, rights, nil)
	if err != nil || n != len(payload) || oobn != len(rights) || unixConn.CloseWrite() != nil {
		return ErrGuardedExecutionUnknown
	}
	raw, err := io.ReadAll(io.LimitReader(unixConn, localMaxFrame+1))
	if err != nil || !fresh() {
		return ErrGuardedExecutionUnknown
	}
	verdict := validateGuardedExecutionReply(req, raw)
	if !fresh() {
		return ErrGuardedExecutionUnknown
	}
	switch verdict {
	case "AUTHORIZED":
		return nil
	case "DENIED":
		return ErrAdmissionRejected
	default:
		return ErrGuardedExecutionUnknown
	}
}

func validateGuardedExecutionReply(req guardedExecutionRequest, raw []byte) string {
	if len(raw) > localMaxFrame || !utf8.Valid(raw) {
		return ""
	}
	reply, ok := exactWireObject(raw)
	if !ok || len(reply) != 2 {
		return ""
	}
	fields, ok := exactWireObject(reply["request"])
	expectedFields := len(guardedExecutionFields)
	if req.Version == 3 {
		expectedFields++
		if fields["lock_offset_digest"] == nil {
			return ""
		}
	}
	if !ok || len(fields) != expectedFields {
		return ""
	}
	for _, name := range guardedExecutionFields {
		if fields[name] == nil {
			return ""
		}
	}
	var echoed guardedExecutionRequest
	var verdict string
	if json.Unmarshal(reply["request"], &echoed) != nil || echoed != req || json.Unmarshal(reply["verdict"], &verdict) != nil {
		return ""
	}
	return verdict
}

// Darwin flock does not by itself distinguish a duplicate descriptor from a
// separately opened descriptor. Seek a duplicate of the held file to a fresh
// offset so the authenticated receiver can prove it shares the open description.
func proveGuardedLockOffset(lock *os.File) (string, error) {
	const lower, upper = 1 << 40, 1 << 61
	delta, err := rand.Int(rand.Reader, big.NewInt(upper-lower))
	if err != nil {
		return "", err
	}
	offset, err := lock.Seek(lower+delta.Int64(), io.SeekStart)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(strconv.AppendInt(nil, offset, 10))
	return hex.EncodeToString(digest[:]), nil
}
