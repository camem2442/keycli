package effectgateway

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sessionTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestClaimSessionKeyBindingAndLifetime(t *testing.T) {
	ctx := context.Background()
	now := testNow
	ring := NewMemoryKeyRing(time.Hour, func() time.Time { return now })
	claim := activeClaim()
	authority := &fakeClaims{snapshot: claim}
	dir := sessionTestDir(t)
	path := filepath.Join(dir, "active.key")

	for _, mismatch := range []struct {
		name   string
		change func(*ClaimSnapshot)
	}{
		{"goal", func(c *ClaimSnapshot) { c.GoalID += "-other" }},
		{"owner", func(c *ClaimSnapshot) { c.Owner += "-other" }},
		{"session", func(c *ClaimSnapshot) { c.Session += "-other" }},
		{"generation", func(c *ClaimSnapshot) { c.Generation++ }},
		{"state", func(c *ClaimSnapshot) { c.State = "RECONCILING" }},
	} {
		t.Run(mismatch.name, func(t *testing.T) {
			wrong := claim
			mismatch.change(&wrong)
			if err := ring.IssueClaim(ctx, authority, wrong, path); !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("mismatched claim issuance: %v", err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("mismatched issuance created a file: %v", err)
			}
		})
	}
	if err := ring.IssueClaim(ctx, authority, claim, path); err != nil {
		t.Fatal(err)
	}
	fileKey, err := ReadSessionKeyFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ring.IssueClaim(ctx, authority, claim, path); err == nil {
		t.Fatal("reissuance overwrote the delivery path")
	}
	if got, err := ring.Key(ctx, claim.GoalID, claim.Owner, claim.Session, claim.Generation); err != nil || !bytes.Equal(got, fileKey) {
		t.Fatal("active key unavailable after failed rotation")
	}
	if got, err := ring.RecoveryKeyForClaim(ctx, claim); err != nil || !bytes.Equal(got, fileKey) {
		t.Fatal("active key unavailable for active claim recovery")
	}
	if _, err := NewMemoryKeyRing(time.Hour, nil).Key(ctx, claim.GoalID, claim.Owner, claim.Session, claim.Generation); err == nil {
		t.Fatal("restarted keyring accepted an old key")
	}

	claim.State = "RECONCILING"
	authority.snapshot = claim
	if _, err := ring.RecoveryKeyForClaim(ctx, claim); err == nil {
		t.Fatal("active key authenticated a claim that transitioned to recovery")
	}
	path = filepath.Join(dir, "recovery.key")
	if err := ring.IssueClaim(ctx, authority, claim, path); err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Key(ctx, claim.GoalID, claim.Owner, claim.Session, claim.Generation); err == nil {
		t.Fatal("recovery key authenticated submit")
	}
	if got, err := ring.RecoveryKeyForClaim(ctx, claim); err != nil || bytes.Equal(got, fileKey) {
		t.Fatal("recovery key was unavailable or reused the old key")
	}
	now = claim.LeaseExpiresAt
	if _, err := ring.RecoveryKey(ctx, claim.GoalID, claim.Owner, claim.Session, claim.Generation); err == nil {
		t.Fatal("key outlived its claim lease")
	}
	if err := ring.IssueClaim(ctx, authority, claim, filepath.Join(dir, "expired.key")); !errors.Is(err, ErrClaimLeaseExpired) {
		t.Fatalf("expired claim issuance: %v", err)
	}
}

func TestSessionKeyFileRejectsLinksAndSharedModes(t *testing.T) {
	dir := sessionTestDir(t)
	ring := NewMemoryKeyRing(time.Minute, nil)
	key, err := ring.Issue("synthetic", "owner", "session", 1)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "session.key")
	link := filepath.Join(dir, "key-link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteSessionKeyFile(link, key); err == nil {
		t.Fatal("writer followed a dangling symlink")
	}
	if err := WriteSessionKeyFile(path, key); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSessionKeyFile(link); err == nil {
		t.Fatal("reader followed a key symlink")
	}
	parentLink := filepath.Join(dir, "parent-link")
	if err := os.Symlink(dir, parentLink); err != nil {
		t.Fatal(err)
	}
	if err := WriteSessionKeyFile(filepath.Join(parentLink, "other.key"), key); err == nil {
		t.Fatal("writer followed a symlink parent")
	}
	if _, err := ReadSessionKeyFile(filepath.Join(parentLink, "session.key")); err == nil {
		t.Fatal("reader followed a symlink parent")
	}
	hardLink := filepath.Join(dir, "key-hard-link")
	if err := os.Link(path, hardLink); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSessionKeyFile(path); err == nil {
		t.Fatal("reader accepted a multiply linked key file")
	}
	if err := os.Remove(hardLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSessionKeyFile(path); err == nil {
		t.Fatal("reader accepted a group-readable key file")
	}
}
