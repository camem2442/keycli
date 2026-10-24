package file

import (
	"context"
	"crypto/rand"
	"errors"

	"github.com/camem2442/keycli/internal/ledger"
)

const ledgerHMACKeyLen = 32

// LedgerHMACKey returns the per-store HMAC key, creating one if missing.
func (s *Store) LedgerHMACKey() ([]byte, error) {
	data, err := s.readStore()
	if err != nil {
		return nil, err
	}
	if data.LedgerHMACKey != "" {
		return ledger.DecodeHMACKey(data.LedgerHMACKey)
	}
	key := make([]byte, ledgerHMACKeyLen)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	data.LedgerHMACKey = ledger.EncodeHMACKey(key)
	if err := s.writeStore(data); err != nil {
		return nil, err
	}
	return key, nil
}

func (s *Store) writeStore(data storeData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeStoreLocked(data)
}

// ErrNoStoreIdentity is returned when the store cannot be unlocked.
var ErrNoStoreIdentity = errors.New("file store locked")

// OpenStoreForLedger returns HMAC key if store is available.
func OpenStoreForLedger(s *Store) ([]byte, error) {
	ok, reason, err := s.Available(context.Background())
	if err != nil {
		return nil, err
	}
	if !ok {
		if reason != "" {
			return nil, errors.New(reason)
		}
		return nil, ErrNoStoreIdentity
	}
	return s.LedgerHMACKey()
}
