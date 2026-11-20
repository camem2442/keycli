// Package applecsv parses a single, explicitly selected Apple Passwords CSV
// export. It never logs credentials and refuses bulk or lossy imports.
package applecsv

import (
	"encoding/csv"
	"errors"
	"io"
	"net/url"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"
)

const maxCSVBytes = 1 << 20

var (
	ErrUnsafeFile  = errors.New("applecsv: file must be owner-only, regular, not linked, and at most 1 MiB")
	ErrInvalidCSV  = errors.New("applecsv: expected exactly one Apple Passwords record with Title,URL,Username,Password,Notes,OTPAuth")
	ErrWrongSite   = errors.New("applecsv: record URL is not HTTPS on the explicitly selected site")
	ErrUnsupported = errors.New("applecsv: notes and OTPAuth must be empty; import those separately")
)

// Credential contains sensitive data. Never print, log, or marshal it into a
// response destined for agents. Callers must only write it to protected storage.
type Credential struct {
	Origin   string `json:"origin"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// ReadOne accepts only a single-row export of one account and an exact host.
// It uses an owner-only, non-symlink file descriptor, not a path-only check.
func ReadOne(path, site string) (Credential, error) {
	if !validSite(site) {
		return Credential{}, ErrWrongSite
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Credential{}, ErrUnsafeFile
	}
	f := os.NewFile(uintptr(fd), "apple-password-export")
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > maxCSVBytes || st.Size() < 1 {
		return Credential{}, ErrUnsafeFile
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); !ok || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return Credential{}, ErrUnsafeFile
	}
	data, err := io.ReadAll(io.LimitReader(f, maxCSVBytes+1))
	if err != nil || len(data) > maxCSVBytes || !utf8.Valid(data) {
		return Credential{}, ErrInvalidCSV
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = 6
	header, err := reader.Read()
	if err != nil || !equalHeaders(header, []string{"Title", "URL", "Username", "Password", "Notes", "OTPAuth"}) {
		return Credential{}, ErrInvalidCSV
	}
	row, err := reader.Read()
	if err != nil || len(row) != 6 || row[2] == "" || row[3] == "" || len(row[2]) > 1024 || len(row[3]) > 4096 {
		return Credential{}, ErrInvalidCSV
	}
	// Never silently discard second-factor material or notes during import.
	if row[4] != "" || row[5] != "" {
		return Credential{}, ErrUnsupported
	}
	if _, err = reader.Read(); err != io.EOF {
		return Credential{}, ErrInvalidCSV
	}
	u, err := url.Parse(row[1])
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || !strings.EqualFold(u.Hostname(), site) || strings.HasSuffix(u.Host, ".") {
		return Credential{}, ErrWrongSite
	}
	return Credential{Origin: "https://" + strings.ToLower(site), Username: row[2], Password: row[3]}, nil
}

func validSite(site string) bool {
	if len(site) < 3 || len(site) > 253 || site[0] == '.' || site[len(site)-1] == '.' || !strings.Contains(site, ".") {
		return false
	}
	for _, label := range strings.Split(site, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

func equalHeaders(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
