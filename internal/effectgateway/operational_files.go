package effectgateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

// ReadPrivateConfig reads a bounded, owner-only configuration with exact fields.
func ReadPrivateConfig(path string, fields map[string]any) error {
	raw, err := ReadPrivateFile(path, maxAuthorityConfig)
	if err != nil || !DecodePrivateConfig(raw, fields) {
		return errAuthorityConfig
	}
	return nil
}

// DecodePrivateConfig decodes exact, non-null fields of a protected JSON object.
func DecodePrivateConfig(raw []byte, fields map[string]any) bool {
	return utf8.Valid(raw) && decodeAuthorityObject(raw, fields, nil)
}

// CheckPrivateClaims checks the protected database path before it is passed to
// the read-only SQLite adapter.
func CheckPrivateClaims(path string) error {
	uid := uint32(os.Geteuid())
	dir, name, err := openAuthorityParent(path, uid, uid)
	if err != nil {
		return errPrivateFile
	}
	defer dir.Close()
	file, err := openOperationalFile(dir, name, unix.O_RDONLY)
	if err != nil {
		return errPrivateFile
	}
	return file.Close()
}

// PrepareOperationalJournal ensures that the journal is on a protected,
// writable, durable directory before accepting traffic.
func PrepareOperationalJournal(path string) error {
	dir, err := openOperationalDirectory(path, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func openOperationalDirectory(path string, create bool) (*os.File, error) {
	uid := uint32(os.Geteuid())
	parent, name, err := openAuthorityParent(path, uid, uid)
	if err != nil {
		return nil, errPrivateFile
	}
	defer parent.Close()
	if create {
		var st unix.Stat_t
		if unix.Fstat(int(parent.Fd()), &st) != nil || st.Uid != uid {
			return nil, errPrivateFile
		}
		err := unix.Mkdirat(int(parent.Fd()), name, 0700)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return nil, errPrivateFile
		}
		if parent.Sync() != nil {
			return nil, errPrivateFile
		}
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errPrivateFile
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uid || st.Mode&07777 != 0700 || !authorityACLProtected(fd) {
		_ = unix.Close(fd)
		return nil, errPrivateFile
	}
	return os.NewFile(uintptr(fd), name), nil
}

func openOperationalFile(dir *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	created := flags&(unix.O_CREAT|unix.O_EXCL) == unix.O_CREAT|unix.O_EXCL
	var chmodErr error
	if created {
		chmodErr = unix.Fchmod(fd, 0600)
	}
	var st unix.Stat_t
	if chmodErr != nil || unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&unix.S_IFMT != unix.S_IFREG ||
		st.Mode&07777 != 0600 || st.Nlink != 1 || !authorityACLProtected(fd) {
		_ = unix.Close(fd)
		if created {
			_ = unix.Unlinkat(int(dir.Fd()), name, 0)
		}
		return nil, errPrivateFile
	}
	return os.NewFile(uintptr(fd), name), nil
}

const (
	issuedTokenIDSize = 32
	issuedValueLimit  = 16 << 10
)

// ErrIssueStoreUncertain indicates that an interrupted publication exists.
// The record cannot authorize a new issuance or confirm a previous one.
var ErrIssueStoreUncertain = errors.New("issued token publication is uncertain")

// PrivateIssueStore records an issued token ID and value together under its
// original mutation ID. Lookup returns only the token ID to the observer.
type PrivateIssueStore struct{ path string }

func OpenPrivateIssueStore(path string) (PrivateIssueStore, error) {
	dir, err := openOperationalDirectory(path, false)
	if err != nil {
		return PrivateIssueStore{}, errPrivateFile
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return PrivateIssueStore{}, errPrivateFile
	}
	return PrivateIssueStore{path: path}, nil
}

func (s PrivateIssueStore) Store(ctx context.Context, operationID, tokenID string, value []byte) error {
	if ctx == nil || ctx.Err() != nil || !journalSafeID(operationID) || !validIssueTokenID(tokenID) || len(value) == 0 || len(value) > issuedValueLimit {
		return errPrivateFile
	}
	dir, err := openOperationalDirectory(s.path, false)
	if err != nil {
		return errPrivateFile
	}
	defer dir.Close()
	// All writers lock the private directory. Rename publishes a fully synced
	// record in one step, and a second writer cannot replace the record.
	dfd := int(dir.Fd())
	if unix.Flock(dfd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return errPrivateFile
	}
	destination := operationID + ".issued"
	var st unix.Stat_t
	if err := unix.Fstatat(dfd, destination, &st, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, os.ErrNotExist) {
		return errPrivateFile
	}
	// Keep any file left by a process interruption identifiable by its
	// mutation. Only the final name is eligible for automatic recovery.
	temp := pendingIssueFile(operationID)
	file, err := openOperationalFile(dir, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return errPrivateFile
	}
	defer func() { _ = unix.Unlinkat(dfd, temp, 0) }()
	_, err = file.WriteString(tokenID + "\n")
	if err == nil {
		_, err = file.Write(value)
	}
	if err == nil {
		err = file.Sync()
	}
	if errors.Join(err, file.Close()) != nil || unix.Renameat(dfd, temp, dfd, destination) != nil || dir.Sync() != nil {
		return errPrivateFile
	}
	return nil
}

func (s PrivateIssueStore) Lookup(ctx context.Context, operationID string) (string, bool, error) {
	if ctx == nil || ctx.Err() != nil || !journalSafeID(operationID) {
		return "", false, errPrivateFile
	}
	dir, err := openOperationalDirectory(s.path, false)
	if err != nil {
		return "", false, errPrivateFile
	}
	defer dir.Close()
	// A visible rename left by an interrupted writer must be durable before
	// recovery treats the private record as committed.
	if unix.Flock(int(dir.Fd()), unix.LOCK_SH|unix.LOCK_NB) != nil || dir.Sync() != nil {
		return "", false, errPrivateFile
	}
	file, err := openOperationalFile(dir, operationID+".issued", unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		pending, err := openOperationalFile(dir, pendingIssueFile(operationID), unix.O_RDONLY)
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		if err != nil {
			return "", false, errPrivateFile
		}
		_ = pending.Close()
		return "", false, ErrIssueStoreUncertain
	}
	if err != nil {
		return "", false, errPrivateFile
	}
	defer file.Close()
	var head [issuedTokenIDSize + 1]byte
	if _, err := io.ReadFull(file, head[:]); err != nil || head[issuedTokenIDSize] != '\n' {
		return "", false, errPrivateFile
	}
	tokenID := string(head[:issuedTokenIDSize])
	var st unix.Stat_t
	if !validIssueTokenID(tokenID) || unix.Fstat(int(file.Fd()), &st) != nil || st.Size <= int64(len(head)) ||
		st.Size > int64(len(head)+issuedValueLimit) {
		return "", false, errPrivateFile
	}
	return tokenID, true, nil
}

func pendingIssueFile(operationID string) string {
	return "." + operationID + ".issued.pending"
}

func validIssueTokenID(tokenID string) bool {
	return len(tokenID) == issuedTokenIDSize && len(bytes.Trim([]byte(tokenID), "0123456789abcdef")) == 0
}
