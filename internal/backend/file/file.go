package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/kcerrors"
	"runtime"
)

func init() {
	backend.Register("file", func(ctx context.Context, instanceName string, spec any) (backend.Backend, error) {
		args, ok := spec.(backend.BackendBuildArgs)
		if !ok {
			return nil, fmt.Errorf("file: invalid build args")
		}
		path := args.Spec.Path
		if path == "" {
			path = filepath.Join(args.Deps.Home, ".kc", "store.age")
		}
		return &Store{
			instance: instanceName,
			path:     path,
			home:     args.Deps.Home,
		}, nil
	})
}

type secretRecord struct {
	Value   string     `json:"value"`
	Note    string     `json:"note,omitempty"`
	Expires *time.Time `json:"expires,omitempty"`
}

type storeData struct {
	Secrets map[string]secretRecord `json:"secrets"`
}

// Store is an age-encrypted file backend.
type Store struct {
	instance string
	path     string
	home     string
	mu       sync.Mutex
}

func (s *Store) InstanceName() string { return s.instance }
func (s *Store) Kind() string         { return "file" }
func (s *Store) Caps() backend.Capabilities {
	return backend.Capabilities{CanWrite: true, CanList: true}
}

func (s *Store) Available(ctx context.Context) (bool, string, error) {
	if _, err := s.loadIdentity(); err != nil {
		if errors.Is(err, kcerrors.ErrLocked) {
			return false, "KC_KEY 없음", nil
		}
		return false, err.Error(), nil
	}
	return true, "", nil
}

func (s *Store) Get(ctx context.Context, name string) ([]byte, error) {
	data, err := s.readStore()
	if err != nil {
		return nil, err
	}
	rec, ok := data.Secrets[strings.ToLower(name)]
	if !ok {
		rec, ok = data.Secrets[name]
	}
	if !ok {
		return nil, fmt.Errorf("%w", kcerrors.ErrNotFound)
	}
	return []byte(rec.Value), nil
}

func (s *Store) List(ctx context.Context) ([]backend.Item, error) {
	data, err := s.readStore()
	if err != nil {
		if errors.Is(err, kcerrors.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var items []backend.Item
	for name, rec := range data.Secrets {
		items = append(items, backend.Item{
			Name:    name,
			Note:    rec.Note,
			Expires: rec.Expires,
		})
	}
	return items, nil
}

func (s *Store) Set(ctx context.Context, name string, value []byte, meta backend.Meta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readStoreLocked()
	if err != nil && !errors.Is(err, os.ErrNotExist) && !errors.Is(err, kcerrors.ErrNotFound) {
		return err
	}
	if data.Secrets == nil {
		data.Secrets = map[string]secretRecord{}
	}
	key := strings.ToLower(name)
	data.Secrets[key] = secretRecord{
		Value:   string(value),
		Note:    meta.Note,
		Expires: meta.Expires,
	}
	return s.writeStoreLocked(data)
}

func (s *Store) Delete(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := s.readStoreLocked()
	if err != nil {
		return err
	}
	key := strings.ToLower(name)
	if _, ok := data.Secrets[key]; !ok {
		return fmt.Errorf("%w", kcerrors.ErrNotFound)
	}
	delete(data.Secrets, key)
	return s.writeStoreLocked(data)
}

func (s *Store) readStore() (storeData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readStoreLocked()
}

func (s *Store) readStoreLocked() (storeData, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return storeData{Secrets: map[string]secretRecord{}}, nil
		}
		return storeData{}, err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH); err != nil {
		return storeData{}, err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	id, err := s.loadIdentity()
	if err != nil {
		return storeData{}, err
	}
	r, err := age.Decrypt(f, id)
	if err != nil {
		return storeData{}, fmt.Errorf("decrypt store: %w", err)
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return storeData{}, err
	}
	var data storeData
	if len(raw) == 0 {
		return storeData{Secrets: map[string]secretRecord{}}, nil
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return storeData{}, err
	}
	if data.Secrets == nil {
		data.Secrets = map[string]secretRecord{}
	}
	return data, nil
}

func (s *Store) writeStoreLocked(data storeData) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	id, err := s.loadIdentity()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "store-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := syscall.Flock(int(tmp.Fd()), syscall.LOCK_EX); err != nil {
		tmp.Close()
		return err
	}

	rec, err := identityRecipient(id)
	if err != nil {
		tmp.Close()
		return err
	}
	w, err := age.Encrypt(tmp, rec)
	if err != nil {
		tmp.Close()
		return err
	}
	enc, err := json.Marshal(data)
	if err != nil {
		w.Close()
		tmp.Close()
		return err
	}
	if _, err := w.Write(enc); err != nil {
		w.Close()
		tmp.Close()
		return err
	}
	if err := w.Close(); err != nil {
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
	return os.Rename(tmpName, s.path)
}

func identityRecipient(id age.Identity) (age.Recipient, error) {
	switch x := id.(type) {
	case *age.X25519Identity:
		return x.Recipient(), nil
	default:
		return nil, fmt.Errorf("unsupported identity type")
	}
}

func (s *Store) loadIdentity() (age.Identity, error) {
	if key := strings.TrimSpace(os.Getenv("KC_KEY")); key != "" {
		id, err := age.ParseX25519Identity(key)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid KC_KEY", kcerrors.ErrLocked)
		}
		return id, nil
	}
	if runtime.GOOS == "darwin" {
		// Age key from keychain item kc.age is NOT VERIFIED on real Mac hardware.
		return nil, fmt.Errorf("%w", kcerrors.ErrLocked)
	}
	return nil, fmt.Errorf("%w", kcerrors.ErrLocked)
}

// GenerateKCKey creates a new age identity string for tests/setup.
func GenerateKCKey() (string, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// KeyFingerprint returns a short non-secret identifier for clipboard checksums.
func KeyFingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}
