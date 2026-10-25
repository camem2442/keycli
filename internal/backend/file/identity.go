package file

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
	"github.com/camem2442/keycli/internal/testhooks"
)

// ErrNoIdentity reports that no age identity exists in KC_KEY, receiver.key, or
// the keychain. Callers that generate a new identity must check for it first so
// an existing identity is never silently replaced.
var ErrNoIdentity = errors.New("no age identity")

// IdentitySource describes where ResolveIdentity found the age identity.
type IdentitySource string

const (
	IdentitySourceEnv       IdentitySource = "env"
	IdentitySourceFile      IdentitySource = "file"
	IdentitySourceKeychain  IdentitySource = "keychain"
	IdentitySourceGenerated IdentitySource = "generated"
)

// IdentityOptions selects the inputs used to resolve an age identity.
type IdentityOptions struct {
	Home     string
	Config   config.Config
	Security backend.SecurityRunner
}

// ResolvedIdentity is an age identity plus the source it came from.
type ResolvedIdentity struct {
	Identity age.Identity
	Source   IdentitySource
	// Path is set when Source is IdentitySourceFile.
	Path string
}

// Recipient returns the public recipient for the identity.
func (r ResolvedIdentity) Recipient() (age.Recipient, error) {
	return identityRecipient(r.Identity)
}

// IdentityString returns the secret identity string (for explicit
// --print-identity output only).
func (r ResolvedIdentity) IdentityString() (string, error) {
	x, ok := r.Identity.(*age.X25519Identity)
	if !ok {
		return "", fmt.Errorf("unsupported identity type")
	}
	return x.String(), nil
}

// ReceiverKeyPath is the default on-disk identity path.
func ReceiverKeyPath(home string) string {
	return filepath.Join(home, ".kc", "receiver.key")
}

// ResolveIdentity loads the first identity found in KC_KEY, receiver.key, then
// the keychain item kc.age. It never creates or modifies a key.
func ResolveIdentity(ctx context.Context, opts IdentityOptions) (ResolvedIdentity, error) {
	if key := strings.TrimSpace(os.Getenv("KC_KEY")); key != "" {
		id, err := age.ParseX25519Identity(key)
		if err != nil {
			return ResolvedIdentity{}, fmt.Errorf("%w: invalid KC_KEY", kcerrors.ErrLocked)
		}
		return ResolvedIdentity{Identity: id, Source: IdentitySourceEnv}, nil
	}

	path := ReceiverKeyPath(opts.Home)
	if data, err := os.ReadFile(path); err == nil {
		key := strings.TrimSpace(string(data))
		id, err := age.ParseX25519Identity(key)
		if err != nil {
			return ResolvedIdentity{}, fmt.Errorf("%w: invalid receiver.key", kcerrors.ErrLocked)
		}
		return ResolvedIdentity{Identity: id, Source: IdentitySourceFile, Path: path}, nil
	}

	if opts.Security != nil && (runtime.GOOS == "darwin" || testhooks.Env(opts.Config, "KC_TEST_KEYCHAIN") == "1") {
		out, err := opts.Security.Run(ctx, "find-generic-password", "-s", ageKeyService, "-a", ageKeyAccount, "-w")
		if err == nil {
			key := strings.TrimSpace(string(out))
			id, err := age.ParseX25519Identity(key)
			if err != nil {
				return ResolvedIdentity{}, fmt.Errorf("%w: invalid age key in keychain", kcerrors.ErrLocked)
			}
			return ResolvedIdentity{Identity: id, Source: IdentitySourceKeychain}, nil
		}
	}

	return ResolvedIdentity{}, ErrNoIdentity
}

// SaveIdentityFile writes a new identity to home/.kc/receiver.key with 0600
// permissions. It refuses to overwrite an existing file (os.ErrExist).
func SaveIdentityFile(home, key string) (string, error) {
	path := ReceiverKeyPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(key + "\n"); err != nil {
		f.Close()
		os.Remove(path)
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", err
	}
	return path, nil
}

// LoadIdentityForImport exposes the age identity for bundle import.
func (s *Store) LoadIdentityForImport() (age.Identity, error) {
	return s.loadIdentity()
}
