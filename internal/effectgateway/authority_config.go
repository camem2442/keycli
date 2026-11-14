package effectgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/camem2442/keycli/internal/hostbridge"
	"golang.org/x/sys/unix"
)

const maxAuthorityConfig = 64 << 10

var errAuthorityConfig = errors.New("invalid or unsafe host authority configuration")

type registrationKey struct{ goal, mutation string }

type hostRegistry map[registrationKey]RegisteredOperation

func (r hostRegistry) Resolve(_ context.Context, goalID, mutationID string) (RegisteredOperation, error) {
	reg, ok := r[registrationKey{goalID, mutationID}]
	if !ok {
		return RegisteredOperation{}, ErrAdmissionRejected
	}
	return reg, nil
}

// LoadLocalAuthority snapshots an owner-only registration file at startup. A
// restart is required to change fixed sockets, peer credentials or registrations.
func LoadLocalAuthority(path string) (LocalAuthority, error) {
	return loadLocalAuthority(path, false)
}

// LoadOperationalAuthority also requires both protected socket entries to
// exist at startup. Each actual request still checks the connected peer.
func LoadOperationalAuthority(path string) (LocalAuthority, error) {
	return loadLocalAuthority(path, true)
}

func loadLocalAuthority(path string, operational bool) (LocalAuthority, error) {
	var result LocalAuthority
	uid := uint32(os.Geteuid())
	dir, name, err := openAuthorityParent(path, uid, uid)
	if err != nil {
		return result, errAuthorityConfig
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return result, errAuthorityConfig
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG ||
		st.Mode&07777 != 0600 || st.Uid != uid || st.Nlink != 1 || st.Size > maxAuthorityConfig || !authorityACLProtected(fd) {
		return result, errAuthorityConfig
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxAuthorityConfig+1))
	if err != nil || len(raw) > maxAuthorityConfig || !utf8.Valid(raw) {
		return result, errAuthorityConfig
	}
	var version int
	var socket string
	var socketUID uint32
	var entries []json.RawMessage
	var guardedRaw json.RawMessage
	if !decodeAuthorityObject(raw, map[string]any{
		"v": &version, "socket_path": &socket, "socket_uid": &socketUID, "registrations": &entries,
	}, map[string]any{"guarded_execution": &guardedRaw}) || version != 1 || len(entries) == 0 ||
		!authoritySocketProtected(socket, uid, socketUID, operational) {
		return result, errAuthorityConfig
	}
	client, err := hostbridge.NewLocalAdmitter(socket, socketUID)
	if err != nil {
		return result, errAuthorityConfig
	}
	var execution *hostbridge.GuardedExecution
	if operational && guardedRaw == nil {
		return result, errAuthorityConfig
	}
	if guardedRaw != nil {
		var guardedSocket string
		var guardedUID uint32
		var guardedPID int32
		if !decodeAuthorityObject(guardedRaw, map[string]any{
			"socket_path": &guardedSocket, "socket_uid": &guardedUID, "socket_pid": &guardedPID,
		}, nil) || !authoritySocketProtected(guardedSocket, uid, guardedUID, operational) {
			return result, errAuthorityConfig
		}
		execution, err = hostbridge.NewGuardedExecution(guardedSocket, guardedUID, int(guardedPID))
		if err != nil {
			return result, errAuthorityConfig
		}
	}

	registry := make(hostRegistry, len(entries))
	for _, entry := range entries {
		var reg RegisteredOperation
		id := &reg.Identity
		if !decodeAuthorityObject(entry, map[string]any{
			"goal_id": &id.GoalID, "mutation_id": &id.MutationID, "operation": &id.OperationID,
			"writer_generation": &id.WriterGeneration, "owner": &id.WriterOwner, "session": &id.WriterSession,
			"provider": &reg.Provider, "provider_operation": &reg.ProviderOperation, "canonical_target": &reg.CanonicalTarget,
			"resource_digest": &reg.ResourceDigest, "plan_digest": &reg.PlanDigest, "grant_digest": &reg.GrantDigest,
		}, nil) {
			return result, errAuthorityConfig
		}
		_, mutating, err := hostbridge.ClassifyOperation(reg.Provider, reg.ProviderOperation)
		if err != nil || !mutating || !reg.authorityBinding().Valid() || strings.TrimSpace(reg.CanonicalTarget) == "" ||
			len(reg.CanonicalTarget) > 4096 || strings.ContainsRune(reg.CanonicalTarget, 0) {
			return result, errAuthorityConfig
		}
		key := registrationKey{id.GoalID, id.MutationID}
		if _, exists := registry[key]; exists {
			return result, errAuthorityConfig
		}
		registry[key] = reg
	}
	return LocalAuthority{Registry: registry, Admitter: client, Execution: execution}, nil
}

func authoritySocketProtected(path string, uid, socketUID uint32, mustExist bool) bool {
	if len(path) > 103 {
		return false
	}
	dir, name, err := openAuthorityParent(path, uid, socketUID)
	if err != nil {
		return false
	}
	defer dir.Close()
	var st unix.Stat_t
	err = unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW)
	return (!mustExist && errors.Is(err, os.ErrNotExist)) ||
		(err == nil && st.Mode&unix.S_IFMT == unix.S_IFSOCK && st.Mode&07777 == 0600 && st.Uid == socketUID)
}

// openAuthorityParent walks the path using directory descriptors, checking that
// no component can be redirected by a symlink or an unrelated user. Sticky
// shared ancestors may contain a protected child; the direct parent must
// never be shared writable.
func openAuthorityParent(path string, uid, serviceUID uint32) (*os.File, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsRune(path, 0) {
		return nil, "", errAuthorityConfig
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, "", err
	}
	var root unix.Stat_t
	if unix.Fstat(fd, &root) != nil || root.Uid != 0 || root.Mode&0022 != 0 || !authorityACLProtected(fd) {
		_ = unix.Close(fd)
		return nil, "", errAuthorityConfig
	}
	for i, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil, "", err
		}
		fd = next
		var st unix.Stat_t
		if unix.Fstat(fd, &st) != nil || (st.Uid != uid && st.Uid != serviceUID && st.Uid != 0) ||
			(st.Mode&0022 != 0 && (st.Mode&unix.S_ISVTX == 0 || i == len(parts)-2)) || !authorityACLProtected(fd) {
			_ = unix.Close(fd)
			return nil, "", errAuthorityConfig
		}
	}
	return os.NewFile(uintptr(fd), path), parts[len(parts)-1], nil
}

// decodeAuthorityObject requires exact names and a single non-null value for
// every required field and any optional fields present.
func decodeAuthorityObject(raw []byte, required, optional map[string]any) bool {
	d := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := d.Token(); err != nil || tok != json.Delim('{') {
		return false
	}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok {
			return false
		}
		target := required[name]
		if target == nil {
			target = optional[name]
		}
		var value json.RawMessage
		if target == nil || d.Decode(&value) != nil || bytes.Equal(value, []byte("null")) || json.Unmarshal(value, target) != nil {
			return false
		}
		delete(required, name)
		delete(optional, name)
	}
	if tok, err := d.Token(); err != nil || tok != json.Delim('}') || len(required) != 0 {
		return false
	}
	_, err := d.Token()
	return err == io.EOF
}
