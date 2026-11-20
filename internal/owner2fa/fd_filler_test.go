package owner2fa

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func socketPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fd, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	a := os.NewFile(uintptr(fd[0]), "sender")
	b := os.NewFile(uintptr(fd[1]), "receiver")
	defer a.Close()
	defer b.Close()
	ac, err := net.FileConn(a)
	if err != nil {
		t.Fatal(err)
	}
	bc, err := net.FileConn(b)
	if err != nil {
		t.Fatal(err)
	}
	return ac.(*net.UnixConn), bc.(*net.UnixConn)
}
func TestFDCodeFillerOneUseSocketpair(t *testing.T) {
	send, recv := socketPair(t)
	defer send.Close()
	defer recv.Close()
	f := &FDCodeFiller{Site: "example.com", Conn: send}
	finished := make(chan error, 1)
	go func() { finished <- f.FillVerifiedTOTP(context.Background(), "example.com", []byte("123456")) }()
	if err := recv.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	n, err := recv.Read(b)
	if err != nil || n != 6 || string(b) != "123456" {
		t.Fatalf("invalid socket frame: n=%d err=%v", n, err)
	}
	if _, err := recv.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := f.FillVerifiedTOTP(context.Background(), "example.com", []byte("999999")); !errors.Is(err, ErrFDNotAdmitted) {
		t.Fatal("replay accepted")
	}
}
func TestFDCodeFillerWrongSiteAndMalformedCode(t *testing.T) {
	for _, tc := range []struct{ site, code string }{
		{"wrong.test", "123456"}, {"example.com", "x23456"}, {"example.com", "12345"},
		{"example.com", "1234567"}, {"EXAMPLE.COM", "123456"},
	} {
		send, recv := socketPair(t)
		f := &FDCodeFiller{Site: "example.com", Conn: send}
		if err := f.FillVerifiedTOTP(context.Background(), tc.site, []byte(tc.code)); !errors.Is(err, ErrFDNotAdmitted) {
			t.Errorf("accepted %q: %v", tc.site, err)
		}
		_ = recv.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
		b := make([]byte, 8)
		if n, _ := recv.Read(b); n != 0 {
			t.Fatal("unauthorized transfer")
		}
		send.Close()
		recv.Close()
	}
}
func TestFDCodeFillerCancelledContext(t *testing.T) {
	send, recv := socketPair(t)
	defer send.Close()
	defer recv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &FDCodeFiller{Site: "example.com", Conn: send}
	if err := f.FillVerifiedTOTP(ctx, "example.com", []byte("123456")); !errors.Is(err, ErrFDNotAdmitted) {
		t.Fatal("cancel not enforced")
	}
}
func TestFDCodeFillerConcurrentOnlyOneTransfer(t *testing.T) {
	send, recv := socketPair(t)
	defer send.Close()
	defer recv.Close()
	f := &FDCodeFiller{Site: "example.com", Conn: send}
	ch := make(chan error, 2)
	go func() { ch <- f.FillVerifiedTOTP(context.Background(), "example.com", []byte("123456")) }()
	go func() { ch <- f.FillVerifiedTOTP(context.Background(), "example.com", []byte("123456")) }()
	_ = recv.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 6)
	if n, _ := recv.Read(b); n != 6 {
		t.Fatal("socket transfer missing")
	}
	if _, err := recv.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	var ok, deny int
	for i := 0; i < 2; i++ {
		if err := <-ch; err == nil {
			ok++
		} else if errors.Is(err, ErrFDNotAdmitted) {
			deny++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || deny != 1 {
		t.Fatalf("ok=%d deny=%d", ok, deny)
	}
}

func TestFDCodeFillerRequiresBrowserAck(t *testing.T) {
	send, recv := socketPair(t)
	defer send.Close()
	defer recv.Close()
	f := &FDCodeFiller{Site: "example.com", Conn: send}
	finished := make(chan error, 1)
	go func() { finished <- f.FillVerifiedTOTP(context.Background(), "example.com", []byte("123456")) }()
	_ = recv.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 6)
	if n, _ := recv.Read(b); n != 6 {
		t.Fatal("missing code transfer")
	}
	if _, err := recv.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, ErrFDNotAdmitted) {
		t.Fatal("invalid ack was accepted")
	}
	if err := f.FillVerifiedTOTP(context.Background(), "example.com", []byte("123456")); !errors.Is(err, ErrFDNotAdmitted) {
		t.Fatal("retry after failed ack")
	}
}
