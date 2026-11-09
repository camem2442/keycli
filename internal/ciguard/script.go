package ciguard

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// VerifyScript returns the independently verified bytes, not the mutable source path.
// Callers must execute only a private copy of these bytes. Python and gh binaries
// remain separately trusted host dependencies.
func VerifyScript(path, expectedDigest string) ([]byte, error) {
	if !filepath.IsAbs(path) || len(expectedDigest) != 64 || strings.ToLower(expectedDigest) != expectedDigest {
		return nil, errors.New("absolute script path and lowercase SHA256 are required")
	}
	if _, err := hex.DecodeString(expectedDigest); err != nil {
		return nil, errors.New("invalid script SHA256")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 64*1024 {
		return nil, errors.New("guard must be a bounded regular non-symlink script")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) > 64*1024 || int64(len(raw)) != info.Size() {
		return nil, errors.New("guard script changed during read")
	}
	found := fmt.Sprintf("%x", sha256.Sum256(raw))
	if found != expectedDigest {
		return nil, errors.New("guard source SHA256 mismatch")
	}
	return raw, nil
}
