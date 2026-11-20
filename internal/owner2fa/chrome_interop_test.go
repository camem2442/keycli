//go:build kc_owner_chrome_interop

package owner2fa

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

// The child process imports only our synthetic Chrome field mock. No real
// browser, password, OTP seed, Playwright or private profile is touched.
const pythonOwnerFixture = `
import socket
from types import SimpleNamespace
from chrome_owner_totp_fd import OwnerFDTOTP

class Field:
    def __init__(self): self.value = None; self.clicks = 0
    def count(self): return 1
    def fill(self, value, **kw): self.value = value
    def click(self, **kw): self.clicks += 1
class Page:
    url = 'https://example.com/mfa'
    def __init__(self): self.otp=Field(); self.submit=Field()
    def locator(self, sel): return self.otp if sel == '#otp' else self.submit

policy = SimpleNamespace(site='example.com',mfa_mode='owner_totp',otp_selector='#otp',otp_submit_selector='#submit')
request = SimpleNamespace(site='example.com', action='reauthenticate', request_id='parent-0001')
page = Page()
with socket.socket(fileno=3) as conn:
    conn.sendall(b'R') # fixture-only receiver-ready barrier
    OwnerFDTOTP(conn,'example.com','parent-0001','child-0001').fill_after_owner_check(page,policy,request)
assert isinstance(page.otp.value, str) and len(page.otp.value) == 6 and page.otp.value.isdecimal()
assert page.submit.clicks == 1
`

func TestGoPythonOwnerChromeSocketInterop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("inherited AF_UNIX socketpair is Unix-only")
	}
	interpreter, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is required for the isolated browser contract")
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve source path")
	}
	scriptDir := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "scripts"))
	if _, err := os.Stat(filepath.Join(scriptDir, "chrome_owner_totp_fd.py")); err != nil {
		t.Fatal("missing owner-only Python receiver source:", err)
	}
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	sendFile := os.NewFile(uintptr(fds[0]), "go-owner-fd")
	childFile := os.NewFile(uintptr(fds[1]), "python-owner-fd")
	defer sendFile.Close()
	defer childFile.Close()
	client, err := net.FileConn(sendFile)
	if err != nil {
		t.Fatal(err)
	}
	conn := client.(*net.UnixConn)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, interpreter, "-B", "-c", pythonOwnerFixture)
	cmd.ExtraFiles = []*os.File{childFile} // fd=3; never a network port
	cmd.Env = append(os.Environ(), "PYTHONPATH="+scriptDir, "PYTHONDONTWRITEBYTECODE=1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Close only this parent's duplicate of the child's receiver handle.
	_ = childFile.Close()
	// Wait until Python has imported the production receiver module and
	// constructed the synthetic browser. This avoids conflating interpreter
	// cold-start with the bounded live OTP write/ACK deadline.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ready [1]byte
	if n, err := conn.Read(ready[:]); err != nil || n != 1 || ready[0] != 'R' {
		_ = cmd.Wait()
		t.Fatalf("owner-only receiver not ready: n=%d err=%v; child=%s", n, err, output.String())
	}
	fill := &FDCodeFiller{Site: "example.com", Conn: conn}
	original := &interopOriginal{}
	grant := &BoundMFAChild{ParentRequestID: "parent-0001", ChildRequestID: "child-0001", Site: "example.com", Inner: original}
	seed := &interopSeed{}
	when := time.Unix(1795172431, 0)
	challenge := Challenge{Site: "example.com", ParentRequestID: "parent-0001", RequestID: "child-0001", Factor: BrokerTOTP, ExpiresAt: when.Add(time.Minute)}
	outcome, fillErr := Fulfill(ctx, when, challenge, grant, seed, fill)
	if fillErr != nil || outcome != Filled {
		_ = cmd.Wait()
		t.Fatalf("Go/Python handshake failed: status=%s err=%v; child=%s", outcome, fillErr, output.String())
	}
	if original.calls != 1 || seed.calls != 1 {
		t.Fatal("child authorization/seed count mismatch")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("synthetic Chrome did not commit OTP input: %v; %s", err, output.String())
	}
	if err := fill.FillVerifiedTOTP(ctx, "example.com", []byte("123456")); !errors.Is(err, ErrFDNotAdmitted) {
		t.Fatalf("replayed a one-use MFA code: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("fixture must never print an OTP")
	}
}

type interopOriginal struct{ calls int }

func (o *interopOriginal) VerifyAndReserve(_ context.Context, c Challenge) error {
	o.calls++
	if c.Site != "example.com" || c.RequestID != "child-0001" || c.ParentRequestID != "parent-0001" {
		return ErrDenied
	}
	return nil
}

type interopSeed struct{ calls int }

func (s *interopSeed) SeedForSite(_ context.Context, _ string) ([]byte, error) {
	s.calls++
	return []byte("12345678901234567890"), nil
}
