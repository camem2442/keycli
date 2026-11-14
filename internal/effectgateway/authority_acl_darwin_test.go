package effectgateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAuthorityConfigDarwinACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	t.Cleanup(func() {
		if out, err := exec.Command("/bin/chmod", "-N", path).CombinedOutput(); err != nil {
			t.Errorf("clear fixture ACL: %v (%s)", err, out)
		}
	})
	for _, tc := range []struct {
		entry     string
		protected bool
	}{
		{"everyone deny delete", true},
		{"everyone allow read", false},
	} {
		if out, err := exec.Command("/bin/chmod", "+a", tc.entry, path).CombinedOutput(); err != nil {
			t.Fatalf("fixture ACL: %v (%s)", err, out)
		}
		if authorityACLProtected(int(file.Fd())) != tc.protected {
			t.Fatalf("unexpected protection for %q", tc.entry)
		}
	}
}
