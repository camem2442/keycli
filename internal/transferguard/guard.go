// Package transferguard checks independently supplied transfer expectations.
// It does not authenticate the sender, receiver host, or transport.
package transferguard

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const MaxBundleBytes int64 = 1 << 20

// ReadVerified checks the bundle bytes against a digest obtained independently.
func ReadVerified(r io.Reader, expectedHex string) ([]byte, error) {
	expected, err := hex.DecodeString(expectedHex)
	if err != nil || len(expected) != sha256.Size {
		return nil, errors.New("expected SHA-256 must be 64 hexadecimal characters")
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBundleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read bundle: %w", err)
	}
	if int64(len(data)) > MaxBundleBytes {
		return nil, errors.New("bundle exceeds size limit")
	}
	got := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(got[:], expected) != 1 {
		return nil, errors.New("bundle SHA-256 mismatch")
	}
	return data, nil
}

// CheckRecipient only compares a locally-derived recipient with the value
// independently verified by the operator. It does not authenticate either value.
func CheckRecipient(local, verified string) error {
	if local == "" || verified == "" || subtle.ConstantTimeCompare([]byte(local), []byte(verified)) != 1 {
		return errors.New("recipient mismatch")
	}
	return nil
}

func SingleExpected(secrets map[string][]byte, expectedName string) ([]byte, error) {
	if expectedName == "" || len(secrets) != 1 {
		return nil, errors.New("bundle must contain exactly the expected item")
	}
	v, ok := secrets[expectedName]
	if !ok {
		return nil, errors.New("bundle item name mismatch")
	}
	return v, nil
}
