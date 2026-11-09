package ciguard

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyScript(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ci_dispatch_guard.py")
	contents := []byte("print('read-only')\n")
	if err := os.WriteFile(file, contents, 0600); err != nil {
		t.Fatal(err)
	}
	sha := fmt.Sprintf("%x", sha256.Sum256(contents))
	if got, err := VerifyScript(file, sha); err != nil || string(got) != string(contents) {
		t.Fatalf("valid script rejected: %s, %v", got, err)
	}
	verified, err := VerifyScript(file, sha)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("print('tampered')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if string(verified) != string(contents) {
		t.Fatal("verified bytes changed when source path changed")
	}
	if _, err := VerifyScript(file, strings.Repeat("0", 64)); err == nil {
		t.Fatal("wrong digest accepted")
	}
	if _, err := VerifyScript("relative.py", sha); err == nil {
		t.Fatal("relative script accepted")
	}
	link := filepath.Join(dir, "redirect.py")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyScript(link, sha); err == nil {
		t.Fatal("symlink accepted")
	}
}
