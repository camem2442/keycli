package file_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/kcerrors"
)

type fakeSecurity struct {
	out []byte
	err error
}

func (f fakeSecurity) Run(context.Context, ...string) ([]byte, error) { return f.out, f.err }
func (f fakeSecurity) RunWithStdin(context.Context, []byte, ...string) ([]byte, error) {
	return f.out, f.err
}

func recipientOf(t *testing.T, key string) string {
	t.Helper()
	id, err := age.ParseX25519Identity(key)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(id.Recipient())
}

func TestResolveIdentityFileThenEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KC_KEY", "")
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	path, err := file.SaveIdentityFile(home, key)
	if err != nil {
		t.Fatal(err)
	}
	if path != file.ReceiverKeyPath(home) {
		t.Fatalf("path %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", info.Mode().Perm())
	}

	res, err := file.ResolveIdentity(context.Background(), file.IdentityOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != file.IdentitySourceFile {
		t.Fatalf("source %q", res.Source)
	}
	rec, err := res.Recipient()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rec) != recipientOf(t, key) {
		t.Fatal("recipient does not match receiver.key")
	}

	envKey, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_KEY", envKey)
	res, err = file.ResolveIdentity(context.Background(), file.IdentityOptions{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != file.IdentitySourceEnv {
		t.Fatalf("source %q", res.Source)
	}
	rec, err = res.Recipient()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rec) != recipientOf(t, envKey) {
		t.Fatal("env identity must take precedence over receiver.key")
	}
}

func TestResolveIdentityKeychain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("KC_KEY", "")
	t.Setenv("KC_TEST_KEYCHAIN", "1")
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	res, err := file.ResolveIdentity(context.Background(), file.IdentityOptions{
		Home:     home,
		Config:   config.Config{AllowTestHooks: true},
		Security: fakeSecurity{out: []byte(key + "\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Source != file.IdentitySourceKeychain {
		t.Fatalf("source %q", res.Source)
	}
	rec, err := res.Recipient()
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(rec) != recipientOf(t, key) {
		t.Fatal("keychain recipient mismatch")
	}
}

func TestResolveIdentityMissing(t *testing.T) {
	t.Setenv("KC_KEY", "")
	_, err := file.ResolveIdentity(context.Background(), file.IdentityOptions{Home: t.TempDir()})
	if !errors.Is(err, file.ErrNoIdentity) {
		t.Fatalf("want ErrNoIdentity, got %v", err)
	}
}

func TestResolveIdentityInvalidEnv(t *testing.T) {
	home := t.TempDir()
	key, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	// A public recipient is not an identity: KC_KEY must fail loudly instead of
	// silently generating or using another key.
	t.Setenv("KC_KEY", recipientOf(t, key))
	_, err = file.ResolveIdentity(context.Background(), file.IdentityOptions{Home: home})
	if !errors.Is(err, kcerrors.ErrLocked) {
		t.Fatalf("want ErrLocked, got %v", err)
	}
}

func TestSaveIdentityFileNoOverwrite(t *testing.T) {
	home := t.TempDir()
	first, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	second, err := file.GenerateKCKey()
	if err != nil {
		t.Fatal(err)
	}
	path, err := file.SaveIdentityFile(home, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.SaveIdentityFile(home, second); !errors.Is(err, os.ErrExist) {
		t.Fatalf("want ErrExist, got %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != strings.TrimSpace(first) {
		t.Fatal("SaveIdentityFile overwrote an existing identity")
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
}
