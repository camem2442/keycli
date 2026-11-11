//go:build darwin || linux

package effectgateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/camem2442/keycli/internal/hostbridge"
	"golang.org/x/sys/unix"
)

const (
	guardedExecutionMethod = "synthetic_guarded_execution"
	guardedExecutionLimit  = 64 << 10
)

var (
	errGuardedPeer    = errors.New("guarded verifier peer mismatch")
	errGuardedNoReply = errors.New("guarded verifier closed without a reply")
	errGuardedReply   = errors.New("invalid guarded verifier reply")
	errGuardedTimeout = errors.New("guarded verifier deadline exceeded")
)

type guardedExecutionRequest struct {
	Version   int    `json:"v"`
	Method    string `json:"method"`
	RequestID string `json:"request_id"`
	hostbridge.AuthorityBinding
}

type guardedExecutionReply struct {
	Request guardedExecutionRequest `json:"request"`
	Result  string                  `json:"result"`
}

func exchangeGuardedExecution(ctx context.Context, path string, uid uint32, pid int, binding hostbridge.AuthorityBinding, lock *os.File) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	info, err := os.Lstat(path)
	if err != nil || !guardedSocketOwnedBy(info, uid) {
		return "", errGuardedPeer
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return "", err
	}
	unixConn := conn.(*net.UnixConn)
	defer unixConn.Close()
	deadline, _ := ctx.Deadline()
	if err := unixConn.SetDeadline(deadline); err != nil {
		return "", err
	}
	stop := context.AfterFunc(ctx, func() { _ = unixConn.Close() })
	defer stop()
	current, err := os.Lstat(path)
	if err != nil || !guardedSocketOwnedBy(current, uid) || !os.SameFile(info, current) || !guardedExecutionPeer(unixConn, uid, pid) {
		return "", errGuardedPeer
	}

	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	req := guardedExecutionRequest{
		Version: 1, Method: guardedExecutionMethod, RequestID: hex.EncodeToString(nonce[:]),
		AuthorityBinding: binding,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	rights := unix.UnixRights(int(lock.Fd()))
	n, oobn, err := unixConn.WriteMsgUnix(payload, rights, nil)
	if err != nil {
		return "", err
	}
	if n != len(payload) || oobn != len(rights) {
		return "", io.ErrShortWrite
	}
	if err := unixConn.CloseWrite(); err != nil {
		return "", err
	}

	raw, err := io.ReadAll(io.LimitReader(unixConn, guardedExecutionLimit+1))
	if ctx.Err() != nil || !time.Now().Before(deadline) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "", errGuardedTimeout
	}
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", errGuardedNoReply
	}
	var reply guardedExecutionReply
	if len(raw) > guardedExecutionLimit || json.Unmarshal(raw, &reply) != nil || reply.Request != req || reply.Result == "" {
		return "", errGuardedReply
	}
	// The synthetic protocol uses one compact encoding. This also rejects
	// duplicate fields and extra fields that encoding/json otherwise accepts.
	canonical, err := json.Marshal(reply)
	if err != nil || !bytes.Equal(raw, canonical) {
		return "", errGuardedReply
	}
	if ctx.Err() != nil || !time.Now().Before(deadline) {
		return "", errGuardedTimeout
	}
	return reply.Result, nil
}

func guardedSocketOwnedBy(info os.FileInfo, uid uint32) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uid && info.Mode() == os.ModeSocket|0600
}
