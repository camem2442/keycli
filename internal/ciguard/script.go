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

// VerifyScript rejects unpinned or redirected Python code before a GitHub token
// can be injected into a child process. It does not authenticate the Python
// interpreter or gh binary; those must be independently trusted host binaries.
func VerifyScript(path, expectedDigest string) (string, error) {
	if !filepath.IsAbs(path) || len(expectedDigest) != 64 || strings.ToLower(expectedDigest) != expectedDigest {
		return "", errors.New("absolute script path and lowercase SHA256 are required")
	}
	if _, err := hex.DecodeString(expectedDigest); err != nil {
		return "", errors.New("invalid script SHA256")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 64*1024 {
		return "", errors.New("guard must be a bounded regular non-symlink script")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(raw) > 64*1024 || int64(len(raw)) != info.Size() {
		return "", errors.New("guard script changed during read")
	}
	found := fmt.Sprintf("%x", sha256.Sum256(raw))
	if found != expectedDigest {
		return "", errors.New("guard source SHA256 mismatch")
	}
	return path, nil
}
