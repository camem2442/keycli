package effectgateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// MemoryKeyRing issues and revokes per-session signing keys.
//
// The gateway is the authority: a writer never supplies its own key, it receives
// one from the host after the claim is established. Keys are bound to
// (goal, owner, session, generation) and expire with the session.
type MemoryKeyRing struct {
	mu   sync.Mutex
	keys map[sessionIdentity]keyEntry
	ttl  time.Duration
	now  func() time.Time
}

type keyEntry struct {
	key          []byte
	expiresAt    time.Time
	recoveryOnly bool
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

// Issue returns a fresh 32-byte key for a synthetic session. Re-issuing for
// the same session replaces the previous key.
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
	id := sessionIdentity{goalID, owner, session, generation}
	clear(m.keys[id].key)
	m.keys[id] = keyEntry{
		key: append([]byte(nil), key...), expiresAt: m.now().Add(m.ttl),
	}
	return key, nil
}

// IssueClaim issues a key for an identity selected by the authenticated host
// operator. Selecting RECONCILING requires operator approval and issues a key
// that can only authenticate recovery requests.
func (m *MemoryKeyRing) IssueClaim(ctx context.Context, claims ClaimAuthority, identity ClaimSnapshot, path string) error {
	if ctx == nil || ctx.Err() != nil || claims == nil || identity.GoalID == "" || identity.Owner == "" ||
		identity.Session == "" || identity.Generation < 1 || (identity.State != "ACTIVE" && identity.State != "RECONCILING") {
		return ErrUnauthenticated
	}
	claim, err := claims.Snapshot(ctx, identity.GoalID)
	if err != nil {
		return ErrAuthorityUnavail
	}
	if claim.GoalID != identity.GoalID || claim.Owner != identity.Owner || claim.Session != identity.Session ||
		claim.Generation != identity.Generation || claim.State != identity.State {
		return ErrUnauthenticated
	}
	key := make([]byte, minKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return ErrAuthorityUnavail
	}
	now := m.now()
	expiresAt := now.Add(m.ttl)
	if !claim.LeaseExpiresAt.IsZero() {
		if !now.Before(claim.LeaseExpiresAt) {
			return ErrClaimLeaseExpired
		}
		if claim.LeaseExpiresAt.Before(expiresAt) {
			expiresAt = claim.LeaseExpiresAt
		}
	}
	if err := WriteSessionKeyFile(path, key); err != nil {
		clear(key)
		return err
	}
	id := sessionIdentity{claim.GoalID, claim.Owner, claim.Session, claim.Generation}
	clear(m.keys[id].key)
	m.keys[id] = keyEntry{
		key: key, expiresAt: expiresAt, recoveryOnly: claim.State == "RECONCILING",
	}
	return nil
}

// Revoke drops the key for one session immediately.
func (m *MemoryKeyRing) Revoke(goalID, owner, session string, generation int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := sessionIdentity{goalID, owner, session, generation}
	clear(m.keys[id].key)
	delete(m.keys, id)
}

// RevokeAll drops every issued key (used on shutdown of a test harness).
func (m *MemoryKeyRing) RevokeAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.keys {
		clear(entry.key)
	}
	clear(m.keys)
}

// Key implements KeyRing.
func (m *MemoryKeyRing) Key(_ context.Context, goalID, owner, session string, generation int64) ([]byte, error) {
	return m.lookup(sessionIdentity{goalID, owner, session, generation}, "ACTIVE")
}

// RecoveryKey accepts keys issued for ACTIVE and RECONCILING claims. Callers
// must still verify the current claim and recovery grant.
func (m *MemoryKeyRing) RecoveryKey(_ context.Context, goalID, owner, session string, generation int64) ([]byte, error) {
	return m.lookup(sessionIdentity{goalID, owner, session, generation}, "")
}

// RecoveryKeyForClaim requires the issued capability to match the current
// claim state. A change between ACTIVE and RECONCILING requires a new key.
func (m *MemoryKeyRing) RecoveryKeyForClaim(ctx context.Context, claim ClaimSnapshot) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || (claim.State != "ACTIVE" && claim.State != "RECONCILING") {
		return nil, ErrUnauthenticated
	}
	if !claim.LeaseExpiresAt.IsZero() && !m.now().Before(claim.LeaseExpiresAt) {
		return nil, ErrClaimLeaseExpired
	}
	return m.lookup(sessionIdentity{claim.GoalID, claim.Owner, claim.Session, claim.Generation}, claim.State)
}

func (m *MemoryKeyRing) lookup(id sessionIdentity, requiredState string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.keys[id]
	if !ok {
		return nil, errors.New("no session key for this claim identity")
	}
	if !m.now().Before(entry.expiresAt) {
		clear(entry.key)
		delete(m.keys, id)
		return nil, errors.New("session key expired")
	}
	if requiredState != "" && entry.recoveryOnly != (requiredState == "RECONCILING") {
		return nil, ErrUnauthenticated
	}
	out := make([]byte, len(entry.key))
	copy(out, entry.key)
	return out, nil
}

var errPrivateFile = errors.New("invalid or unsafe private file")

// ReadPrivateFile reads a bounded owner-only file under a protected path.
func ReadPrivateFile(path string, maxBytes int) ([]byte, error) {
	if maxBytes < 1 {
		return nil, errPrivateFile
	}
	uid := uint32(os.Geteuid())
	dir, name, err := openAuthorityParent(path, uid, uid)
	if err != nil {
		return nil, errPrivateFile
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errPrivateFile
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&07777 != 0600 ||
		st.Uid != uid || st.Nlink != 1 || st.Size > int64(maxBytes) || !authorityACLProtected(fd) {
		return nil, errPrivateFile
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil || len(raw) > maxBytes {
		return nil, errPrivateFile
	}
	return raw, nil
}

// ReadSessionKeyFile reads the exact 32-byte hex format used for product keys.
func ReadSessionKeyFile(path string) ([]byte, error) {
	raw, err := ReadPrivateFile(path, hex.EncodedLen(minKeyBytes)+1)
	if err != nil {
		return nil, err
	}
	defer clear(raw)
	if len(raw) == hex.EncodedLen(minKeyBytes)+1 && raw[len(raw)-1] == '\n' {
		raw = raw[:len(raw)-1]
	}
	if len(raw) != hex.EncodedLen(minKeyBytes) {
		return nil, errPrivateFile
	}
	key := make([]byte, minKeyBytes)
	if _, err := hex.Decode(key, raw); err != nil {
		clear(key)
		return nil, errPrivateFile
	}
	return key, nil
}

// WriteSessionKeyFile creates a private file without following links or
// replacing any existing entry. The caller delivers this file out of band.
func WriteSessionKeyFile(path string, key []byte) error {
	if len(key) != minKeyBytes {
		return errPrivateFile
	}
	uid := uint32(os.Geteuid())
	dir, name, err := openAuthorityParent(path, uid, uid)
	if err != nil {
		return errPrivateFile
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return errPrivateFile
	}
	file := os.NewFile(uintptr(fd), name)
	err = file.Chmod(0600)
	if err == nil && !authorityACLProtected(fd) {
		err = errPrivateFile
	}
	if err == nil {
		_, err = file.WriteString(HexKey(key) + "\n")
	}
	if err == nil {
		err = file.Sync()
	}
	if errors.Join(err, file.Close()) != nil {
		_ = unix.Unlinkat(int(dir.Fd()), name, 0)
		return errPrivateFile
	}
	return nil
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
