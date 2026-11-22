package owner2fa

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// FDCodeFiller delivers one independently-authorized OTP to the trusted Mac
// browser process through a private, inherited AF_UNIX socketpair. A remote
// agent has no access to either FD, code, seed, CDP endpoint or browser.
// This is a transport adapter, NOT a credential store or an Authority.
type FDCodeFiller struct {
	Site string
	Conn *net.UnixConn
	mu   sync.Mutex
	used bool
}

var ErrFDNotAdmitted = errors.New("owner2fa: fd transfer denied")

// FillVerifiedTOTP must be invoked by Fulfill after trusted owner Authority
// verification. A failure may have partly delivered bytes: do not retry.
func (f *FDCodeFiller) FillVerifiedTOTP(ctx context.Context, site string, code []byte) error {
	if f == nil {
		return ErrFDNotAdmitted
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.used || f.Conn == nil || site != f.Site || !validSite(site) || len(code) != 6 || ctx.Err() != nil {
		return ErrFDNotAdmitted
	}
	for _, d := range code {
		if d < '0' || d > '9' {
			return ErrFDNotAdmitted
		}
	}
	f.used = true
	if err := f.Conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return ErrFDNotAdmitted
	}
	n, err := f.Conn.Write(code)
	if err != nil || n != len(code) {
		return ErrFDNotAdmitted
	}
	// Writing bytes does not prove Chrome filled the code input.
	// Require a fixed acknowledgement from the owner-only input sink.
	if err := f.Conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return ErrFDNotAdmitted
	}
	var ack [1]byte
	n, err = f.Conn.Read(ack[:])
	if err != nil || n != 1 || ack[0] != 1 {
		return ErrFDNotAdmitted
	}
	return nil
}
