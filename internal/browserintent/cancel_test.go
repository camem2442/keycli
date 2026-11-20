package browserintent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type cancelOnReserve struct {
	cancel context.CancelFunc
	calls  int
}

func (a *cancelOnReserve) VerifyAndReserve(_ context.Context, _ Intent) error {
	a.calls++
	a.cancel()
	return nil
}
func TestGateNeverInvokesAuthorityOnCancelledContext(t *testing.T) {
	intent, err := Decode(strings.NewReader(validJSON()), now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	auth := &permit{allow: true}
	browser := &worker{result: Performed}
	_, err = Gate(ctx, now, intent, auth, browser)
	if !errors.Is(err, ErrRejected) || auth.called != 0 || browser.calls != 0 {
		t.Fatalf("cancel bypass: err=%v authority=%d browser=%d", err, auth.called, browser.calls)
	}
}
func TestGateNeverInvokesBrowserAfterReservationCancellation(t *testing.T) {
	intent, err := Decode(strings.NewReader(validJSON()), now)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	auth := &cancelOnReserve{cancel: cancel}
	browser := &worker{result: Performed}
	_, err = Gate(ctx, now, intent, auth, browser)
	if !errors.Is(err, ErrRejected) || auth.calls != 1 || browser.calls != 0 {
		t.Fatalf("post-reserve cancel bypass: err=%v authority=%d browser=%d", err, auth.calls, browser.calls)
	}
}
