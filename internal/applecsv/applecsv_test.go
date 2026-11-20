package applecsv

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const header = "Title,URL,Username,Password,Notes,OTPAuth\n"
const secret = "SYNTHETIC-ONLY-NOT-A-REAL-SECRET"

func fixture(t *testing.T, csv string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "selected.csv")
	if err := os.WriteFile(path, []byte(csv), perm); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadOneValid(t *testing.T) {
	path := fixture(t, "\ufeff"+header+"Example,https://example.com/login,alice,"+secret+",,\n", 0600)
	got, err := ReadOne(path, "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Origin != "https://example.com" || got.Username != "alice" || got.Password != secret {
		t.Fatal("valid credentials not preserved")
	}
}

func TestReadOneRejectsUnsafeAndLossyInput(t *testing.T) {
	cases := []struct {
		name, body, site string
		want             error
	}{
		{"wrong header", "bad\nvalue\n", "example.com", ErrInvalidCSV},
		{"bulk", header + "A,https://example.com,u,p,,\nB,https://example.com,u2,p2,,\n", "example.com", ErrInvalidCSV},
		{"otp", header + "A,https://example.com,u,p,,otpauth://totp/example?secret=ABCDEFG\n", "example.com", ErrUnsupported},
		{"notes", header + "A,https://example.com,u,p,personal note,\n", "example.com", ErrUnsupported},
		{"lookalike", header + "A,https://example.com.evil.test,u,p,,\n", "example.com", ErrWrongSite},
		{"http", header + "A,http://example.com,u,p,,\n", "example.com", ErrWrongSite},
		{"userinfo", header + "A,https://example.com@evil.test,u,p,,\n", "example.com", ErrWrongSite},
		{"url query", header + "A,https://example.com/login?ticket=abc,u,p,,\n", "example.com", ErrWrongSite},
		{"port", header + "A,https://example.com:8443,u,p,,\n", "example.com", ErrWrongSite},
		{"no password", header + "A,https://example.com,u,,,\n", "example.com", ErrInvalidCSV},
		{"invalid site", header + "A,https://example.com,u,p,,\n", "*.example.com", ErrWrongSite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := fixture(t, tc.body, 0600)
			_, err := ReadOne(path, tc.site)
			if !errors.Is(err, tc.want) {
				t.Fatalf("wrong error class: %v", err)
			}
		})
	}
}

func TestReadOneRejectsSymlinkAndWorldReadable(t *testing.T) {
	path := fixture(t, header+"A,https://example.com,u,p,,\n", 0600)
	link := filepath.Join(t.TempDir(), "link.csv")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOne(link, "example.com"); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("symlink: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadOne(path, "example.com"); !errors.Is(err, ErrUnsafeFile) {
		t.Fatalf("mode: %v", err)
	}
}

func TestReadOneNoSecretInError(t *testing.T) {
	path := fixture(t, header+"A,https://evil.test,u,"+secret+",,\n", 0600)
	_, err := ReadOne(path, "example.com")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("secret leaked into error: %v", err)
	}
}
