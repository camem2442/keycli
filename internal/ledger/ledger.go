package ledger

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Entry records a push export without storing plaintext.
type Entry struct {
	Name       string    `json:"name"`
	Target     string    `json:"target"`
	HMAC       string    `json:"hmac"`
	ExportedAt time.Time `json:"exported_at"`
}

// File holds ledger entries on disk.
type File struct {
	Path string
}

// Load reads all ledger entries.
func (f File) Load() ([]Entry, error) {
	data, err := os.ReadFile(f.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []Entry
	if len(data) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// Save writes entries atomically.
func (f File) Save(entries []Entry) error {
	unlock, err := f.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return f.saveLocked(entries)
}

func (f File) saveLocked(entries []Entry) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), "ledger-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, f.Path)
}

// lock serializes read-modify-write updates of the ledger across processes.
func (f File) lock() (func(), error) {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(f.Path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}

// Path returns the default ledger file path (~/.kc/ledger.json).
func Path(home string) string {
	if home == "" {
		home = "."
	}
	return filepath.Join(home, ".kc", "ledger.json")
}

// ComputeHMAC returns hex HMAC-SHA256 of value with key.
func ComputeHMAC(key, value []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(value)
	return hex.EncodeToString(m.Sum(nil))
}

// RecordItem is one name/target/value to record in the ledger.
type RecordItem struct {
	Name   string
	Target string
	Value  []byte
}

// Record upserts a ledger entry for name/target with the given value.
func (f File) Record(key []byte, name, target string, value []byte) error {
	return f.RecordAll(key, []RecordItem{{Name: name, Target: target, Value: value}})
}

// RecordAll upserts many entries with a single load and save. push uses this so
// a batch of N secrets costs one ledger rewrite instead of N.
func (f File) RecordAll(key []byte, items []RecordItem) error {
	if len(items) == 0 {
		return nil
	}
	if len(key) == 0 {
		return errors.New("ledger: empty HMAC key")
	}
	unlock, err := f.lock()
	if err != nil {
		return err
	}
	defer unlock()
	entries, err := f.Load()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, item := range items {
		name := strings.ToLower(strings.TrimSpace(item.Name))
		target := strings.TrimSpace(item.Target)
		h := ComputeHMAC(key, item.Value)
		found := false
		for i, e := range entries {
			if strings.EqualFold(e.Name, name) && e.Target == target {
				entries[i].HMAC = h
				entries[i].ExportedAt = now
				found = true
				break
			}
		}
		if !found {
			entries = append(entries, Entry{
				Name:       name,
				Target:     target,
				HMAC:       h,
				ExportedAt: now,
			})
		}
	}
	return f.saveLocked(entries)
}

// Stale returns true if value differs from the last recorded export for name/target.
func (f File) Stale(key []byte, name, target string, value []byte) bool {
	if len(key) == 0 {
		return false
	}
	entries, err := f.Load()
	if err != nil {
		return false
	}
	name = strings.ToLower(strings.TrimSpace(name))
	target = strings.TrimSpace(target)
	h := ComputeHMAC(key, value)
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) && e.Target == target {
			return e.HMAC != h
		}
	}
	return false
}

// StaleAnyTarget reports whether any ledger entry for name has a different HMAC.
func (f File) StaleAnyTarget(key []byte, name string, value []byte) bool {
	if len(key) == 0 {
		return false
	}
	entries, err := f.Load()
	if err != nil {
		return false
	}
	name = strings.ToLower(strings.TrimSpace(name))
	h := ComputeHMAC(key, value)
	for _, e := range entries {
		if strings.EqualFold(e.Name, name) && e.HMAC != h {
			return true
		}
	}
	return false
}

// DecodeHMACKey parses a base64-encoded key from the file store.
func DecodeHMACKey(b64 string) ([]byte, error) {
	b64 = strings.TrimSpace(b64)
	if b64 == "" {
		return nil, errors.New("empty key")
	}
	return base64.StdEncoding.DecodeString(b64)
}

// EncodeHMACKey base64-encodes a key for storage.
func EncodeHMACKey(key []byte) string {
	return base64.StdEncoding.EncodeToString(key)
}

// ErrNoLedgerKey indicates the file store has no ledger HMAC key yet.
var ErrNoLedgerKey = errors.New("ledger HMAC key missing")

// FormatStaleHint builds a human-readable stale marker for ls.
func FormatStaleHint(stale bool) string {
	if stale {
		return " (재전송 필요)"
	}
	return ""
}

// ListStale returns canonical names that differ from any recorded export.
func (f File) ListStale(key []byte, names []string, getValue func(name string) ([]byte, error)) ([]string, error) {
	var out []string
	for _, name := range names {
		val, err := getValue(name)
		if err != nil {
			continue
		}
		if f.StaleAnyTarget(key, name, val) {
			out = append(out, name)
		}
	}
	return out, nil
}

// StatusLine formats one status row.
func StatusLine(name, target string, exportedAt time.Time) string {
	return fmt.Sprintf("%s → %s (exported %s)", name, target, exportedAt.Format(time.RFC3339))
}
