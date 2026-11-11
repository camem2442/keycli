package journal

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const recordLimit = 4096

func (s Store) open(create bool) (*os.File, error) {
	if !filepath.IsAbs(s.Root) || filepath.Clean(s.Root) != s.Root || s.Root == "/" {
		return nil, errors.New("delivery journal requires an absolute clean directory path")
	}
	parts := strings.Split(strings.TrimPrefix(s.Root, "/"), "/")
	parent, err := openDirectory(unix.AT_FDCWD, "/")
	if err != nil {
		return nil, err
	}
	defer func() { _ = parent.Close() }()
	for _, part := range parts[:len(parts)-1] {
		next, err := openDirectory(int(parent.Fd()), part)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && !create {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("open delivery journal parent: %w", err)
		}
		_ = parent.Close()
		parent = next
		info, err := stat(parent)
		if err != nil {
			return nil, err
		}
		// A sticky shared ancestor such as /tmp may contain a private parent.
		if (info.Uid != uint32(os.Geteuid()) && info.Uid != 0) ||
			(info.Mode&0022 != 0 && info.Mode&unix.S_ISVTX == 0) {
			return nil, errors.New("unsafe delivery journal ancestor")
		}
	}
	parentInfo, err := stat(parent)
	if err != nil {
		return nil, err
	}
	if parentInfo.Uid != uint32(os.Geteuid()) || parentInfo.Mode&0022 != 0 {
		return nil, errors.New("unsafe delivery journal parent")
	}
	name := parts[len(parts)-1]
	if create {
		err := unix.Mkdirat(int(parent.Fd()), name, 0700)
		if err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create delivery journal: %w", err)
		}
	}
	dir, err := openDirectory(int(parent.Fd()), name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open delivery journal: %w", err)
	}
	info, err := stat(dir)
	if err == nil && (info.Uid != uint32(os.Geteuid()) || info.Mode&07777 != 0700) {
		err = errors.New("delivery journal must be owned by this user with mode 0700")
	}
	if err == nil && create {
		err = parent.Sync()
	}
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	return dir, nil
}

func openDirectory(parent int, name string) (*os.File, error) {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func (s Store) lock(create bool) (*os.File, func(), error) {
	dir, err := s.open(create)
	if err != nil {
		return nil, nil, err
	}
	flags := unix.O_RDWR
	if create {
		flags |= unix.O_CREAT
	}
	lock, err := openPrivateFile(dir, ".lock", flags)
	if err != nil {
		_ = dir.Close()
		return nil, nil, err
	}
	unlock := func() {
		_ = lock.Close()
		_ = dir.Close()
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		unlock()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, nil, ErrBusy
		}
		return nil, nil, err
	}
	// Persist any visible rename left by a prior failed directory sync before
	// allowing a new operation to rely on that terminal state.
	if err := dir.Sync(); err != nil {
		unlock()
		return nil, nil, err
	}
	return dir, unlock, nil
}

func openPrivateFile(dir *os.File, name string, flags int) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, flags|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := stat(file)
	if err == nil && (info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&07777 != 0600 ||
		info.Uid != uint32(os.Geteuid()) || info.Nlink != 1) {
		err = errors.New("unsafe delivery journal file")
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func stat(file *os.File) (unix.Stat_t, error) {
	var info unix.Stat_t
	err := unix.Fstat(int(file.Fd()), &info)
	return info, err
}

func readRecord(dir *os.File, id string) (Record, error) {
	if !operationID.MatchString(id) {
		return Record{}, errors.New("invalid delivery operation record name")
	}
	file, err := openPrivateFile(dir, id+".json", unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, recordLimit+1))
	if err != nil {
		return Record{}, err
	}
	var record Record
	if len(data) > recordLimit || json.Unmarshal(data, &record) != nil ||
		record.OperationID != id || !validIntent(record.Intent) || !validBinding(record) || !validState(record) {
		return Record{}, errors.New("invalid delivery journal record")
	}
	// This also rejects unknown or duplicate fields and trailing input.
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(data, canonical) {
		return Record{}, errors.New("noncanonical delivery journal record")
	}
	return record, nil
}

func validState(record Record) bool {
	switch record.State {
	case IntentWritten, Dispatched, UnknownEffect:
		return record.ProofDigest == ""
	case Acknowledged, ObservedApplied, ObservedNoEffect:
		return digest.MatchString(record.ProofDigest)
	default:
		return false
	}
}

func writeRecord(dir *os.File, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := ".tmp-" + hex.EncodeToString(nonce[:])
	file, err := openPrivateFile(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Unlinkat(int(dir.Fd()), name, 0) }()
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	if err := unix.Renameat(int(dir.Fd()), name, int(dir.Fd()), record.OperationID+".json"); err != nil {
		return err
	}
	return dir.Sync()
}
