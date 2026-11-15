//go:build darwin || linux

package hostbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestGuardedLockOffsetProof(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("offset proof is used on macOS")
	}
	path := filepath.Join(t.TempDir(), ".lock")
	held, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := unix.Flock(int(held.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	dupFD, err := unix.FcntlInt(held.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := os.NewFile(uintptr(dupFD), "guard-duplicate")
	defer duplicate.Close()
	separate, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer separate.Close()

	digest, err := proveGuardedLockOffset(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	offset, err := held.Seek(0, io.SeekCurrent)
	if err != nil || offset < 1<<40 || offset >= 1<<61 {
		t.Fatalf("shared offset: %d, %v", offset, err)
	}
	expected := sha256.Sum256([]byte(strconv.FormatInt(offset, 10)))
	if digest != hex.EncodeToString(expected[:]) {
		t.Fatalf("wrong digest for shared offset: %s", digest)
	}
	separateOffset, err := separate.Seek(0, io.SeekCurrent)
	if err != nil || separateOffset != 0 {
		t.Fatalf("separately opened offset: %d, %v", separateOffset, err)
	}
	info, err := held.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 0 {
		t.Fatalf("lock offset proof changed file contents: size %d", info.Size())
	}
}

func TestGuardedExecutionOffsetEcho(t *testing.T) {
	v2 := guardedExecutionRequest{
		Version: 2, Method: guardedExecutionMethod, RequestID: strings.Repeat("a", 32),
		AuthorityBinding: reservationFixtureBinding(), ExpiresAt: "2026-11-11T12:01:00Z",
	}
	v3 := v2
	v3.Version, v3.LockOffsetDigest = 3, strings.Repeat("b", 64)

	for _, tc := range []struct {
		name    string
		request guardedExecutionRequest
		echo    guardedExecutionRequest
		want    string
	}{
		{"v2 accepted", v2, v2, "AUTHORIZED"},
		{"v3 accepted", v3, v3, "AUTHORIZED"},
		{"v2 injected proof rejected", v2, guardedWithDigest(v2, v3.LockOffsetDigest), ""},
		{"v3 missing proof rejected", v3, guardedWithDigest(v3, ""), ""},
		{"v3 wrong proof rejected", v3, guardedWithDigest(v3, strings.Repeat("c", 64)), ""},
		{"v3 downgrade rejected", v3, v2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply, err := json.Marshal(struct {
				Request guardedExecutionRequest `json:"request"`
				Verdict string                  `json:"verdict"`
			}{tc.echo, "AUTHORIZED"})
			if err != nil {
				t.Fatal(err)
			}
			if got := validateGuardedExecutionReply(tc.request, reply); got != tc.want {
				t.Fatalf("verdict = %q, want %q", got, tc.want)
			}
		})
	}
}

func guardedWithDigest(request guardedExecutionRequest, digest string) guardedExecutionRequest {
	request.LockOffsetDigest = digest
	return request
}
