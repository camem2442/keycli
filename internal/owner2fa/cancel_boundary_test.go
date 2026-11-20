package owner2fa

import (
	"context"
	"errors"
	"testing"
	"time"
)

var clock = time.Unix(1795172431, 0)

func liveChild() Challenge {
	return Challenge{Site: "example.com", RequestID: "child-0001", ParentRequestID: "parent-0001", Factor: BrokerTOTP, ExpiresAt: clock.Add(time.Minute)}
}

type cancelOriginal struct {
	calls  int
	cancel func()
}

func (a *cancelOriginal) VerifyAndReserve(context.Context, Challenge) error {
	a.calls++
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

type cancelSeed struct {
	calls  int
	cancel func()
}

func (s *cancelSeed) SeedForSite(context.Context, string) ([]byte, error) {
	s.calls++
	if s.cancel != nil {
		s.cancel()
	}
	return []byte("12345678901234567890"), nil
}

type cancelFill struct {
	calls  int
	cancel func()
}

func (f *cancelFill) FillVerifiedTOTP(context.Context, string, []byte) error {
	f.calls++
	if f.cancel != nil {
		f.cancel()
	}
	return nil
}
func TestCanceledBeforeMFAAuthorizationDoesNotReadSeed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := &cancelOriginal{}
	s := &cancelSeed{}
	f := &cancelFill{}
	_, err := Fulfill(ctx, clock, liveChild(), a, s, f)
	if !errors.Is(err, ErrDenied) || a.calls != 0 || s.calls != 0 || f.calls != 0 {
		t.Fatalf("canceled bypass: %v auth=%d seed=%d fill=%d", err, a.calls, s.calls, f.calls)
	}
}
func TestCanceledAfterMFAReservationDoesNotReadSeed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &cancelOriginal{cancel: cancel}
	s := &cancelSeed{}
	f := &cancelFill{}
	_, err := Fulfill(ctx, clock, liveChild(), a, s, f)
	if !errors.Is(err, ErrDenied) || a.calls != 1 || s.calls != 0 || f.calls != 0 {
		t.Fatalf("post grant cancellation: %v auth=%d seed=%d fill=%d", err, a.calls, s.calls, f.calls)
	}
}
func TestCanceledDuringSeedReadDoesNotFill(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &cancelOriginal{}
	s := &cancelSeed{cancel: cancel}
	f := &cancelFill{}
	_, err := Fulfill(ctx, clock, liveChild(), a, s, f)
	if !errors.Is(err, ErrDenied) || a.calls != 1 || s.calls != 1 || f.calls != 0 {
		t.Fatalf("post seed cancellation: %v auth=%d seed=%d fill=%d", err, a.calls, s.calls, f.calls)
	}
}
func TestCanceledDuringBrowserInputDoesNotReportFilled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &cancelOriginal{}
	s := &cancelSeed{}
	f := &cancelFill{cancel: cancel}
	status, err := Fulfill(ctx, clock, liveChild(), a, s, f)
	if !errors.Is(err, ErrDenied) || status != "" {
		t.Fatalf("input after cancellation incorrectly reported success: %s, %v", status, err)
	}
}
func TestCanceledApplePasskeyNoManualStatus(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := liveChild()
	c.Factor = Passkey
	a := &cancelOriginal{}
	status, err := Fulfill(ctx, clock, c, a, nil, nil)
	if !errors.Is(err, ErrDenied) || a.calls != 0 || status != "" {
		t.Fatalf("passkey cancel bypass: %s %v", status, err)
	}
}
