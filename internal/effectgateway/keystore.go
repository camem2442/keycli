package effectgateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// MemoryKeyRing issues and revokes per-session signing keys.
//
// The gateway is the authority: a writer never supplies its own key, it receives
// one from the host after the claim is established. Keys are bound to
// (goal, owner, session, generation) and expire with the session.
type MemoryKeyRing struct {
	mu     sync.Mutex
	keys   map[sessionIdentity]keyEntry
	ttl    time.Duration
	now    func() time.Time
	issued int
}

type keyEntry struct {
	key       []byte
	expiresAt time.Time
}

// NewMemoryKeyRing creates a ring with a default session lifetime.
func NewMemoryKeyRing(ttl time.Duration, now func() time.Time) *MemoryKeyRing {
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &MemoryKeyRing{keys: map[sessionIdentity]keyEntry{}, ttl: ttl, now: now}
}

type sessionIdentity struct {
	goal, owner, session string
	generation           int64
}

// Issue returns a fresh 32-byte key for one session. Re-issuing for the same
// session replaces the previous key (rotation).
func (m *MemoryKeyRing) Issue(goalID, owner, session string, generation int64) ([]byte, error) {
	if goalID == "" || owner == "" || session == "" || generation < 1 {
		return nil, errors.New("session key requires goal, owner, session and generation")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[sessionIdentity{goalID, owner, session, generation}] = keyEntry{
		key: append([]byte(nil), key...), expiresAt: m.now().Add(m.ttl),
	}
	m.issued++
	return key, nil
}

// Revoke drops the key for one session immediately.
func (m *MemoryKeyRing) Revoke(goalID, owner, session string, generation int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, sessionIdentity{goalID, owner, session, generation})
}

// RevokeAll drops every issued key (used on shutdown of a test harness).
func (m *MemoryKeyRing) RevokeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys = map[sessionIdentity]keyEntry{}
}

// Key implements KeyRing.
func (m *MemoryKeyRing) Key(_ context.Context, goalID, owner, session string, generation int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.keys[sessionIdentity{goalID, owner, session, generation}]
	if !ok {
		return nil, errors.New("no session key for this claim identity")
	}
	if !m.now().Before(entry.expiresAt) {
		delete(m.keys, sessionIdentity{goalID, owner, session, generation})
		return nil, errors.New("session key expired")
	}
	out := make([]byte, len(entry.key))
	copy(out, entry.key)
	return out, nil
}

// HexKey renders a key for out-of-band delivery to the operator.
func HexKey(key []byte) string { return hex.EncodeToString(key) }

// ParseHexKey accepts a hex-encoded session key.
func ParseHexKey(value string) ([]byte, error) {
	key, err := hex.DecodeString(value)
	if err != nil || len(key) < minKeyBytes {
		return nil, errors.New("session key must be at least 32 bytes of hex")
	}
	return key, nil
}
