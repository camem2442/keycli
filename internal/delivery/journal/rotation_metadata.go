package journal

import (
	"bytes"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const rotationMetadataLimit = 128 << 10

var ErrMetadataChanged = errors.New("rotation metadata changed")

// ReadRotationMetadata reads the host's rotation metadata alongside the
// journal. It does not interpret the metadata as an execution record.
func (s Store) ReadRotationMetadata(planID string) ([]byte, error) {
	if !ValidDigest(planID) {
		return nil, errors.New("invalid rotation metadata identifier")
	}
	dir, err := s.open(false)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return readRotationMetadata(dir, rotationMetadataName(planID))
}

// ReplaceRotationMetadata serializes metadata updates with journal dispatch.
// The caller supplies the exact previous snapshot, or nil on creation.
func (s Store) ReplaceRotationMetadata(planID string, previous, next []byte) error {
	if !ValidDigest(planID) || len(next) == 0 || len(next) > rotationMetadataLimit {
		return errors.New("invalid rotation metadata")
	}
	dir, _, unlock, err := s.lock(true)
	if err != nil {
		return err
	}
	defer unlock()
	name := rotationMetadataName(planID)
	current, err := readRotationMetadata(dir, name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if bytes.Equal(current, next) {
		return nil
	}
	if !bytes.Equal(current, previous) {
		return ErrMetadataChanged
	}
	return writeNamedBytes(dir, name, next, (*os.File).Sync)
}

func rotationMetadataName(planID string) string { return ".rotation-" + planID }

func readRotationMetadata(dir *os.File, name string) ([]byte, error) {
	file, err := openPrivateFile(dir, name, unix.O_RDONLY)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, rotationMetadataLimit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > rotationMetadataLimit {
		return nil, errors.New("invalid rotation metadata size")
	}
	return raw, nil
}
