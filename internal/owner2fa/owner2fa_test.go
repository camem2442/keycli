package owner2fa

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

var testTime = time.Unix(1795172431, 0)

// RFC 6238 Appendix B test vector at 59 seconds. These keys are public vectors.
func TestRFCTOTPVector(t *testing.T) {
	got := totpSHA1([]byte("12345678901234567890"), uint64(59/30), 8)
	if string(got) != "94287082" {
		t.Fatalf("RFC vector mismatch: %q", got)
	}
	got = totpSHA1([]byte("12345678901234567890"), uint64(59/30), 6)
	if string(got) != "287082" {
		t.Fatalf("RFC six-digit mismatch: %q", got)
	}
}

type fakeAuthority struct {
	deny bool
	n    int
}

func (f *fakeAuthority) VerifyAndReserve(_ context.Context, _ Challenge) error {
	f.n++
	if f.deny {
		return errors.New("fake secret")
	}
	return nil
}

type fakeSeeds struct {
	n   int
	key []byte
}

func (f *fakeSeeds) SeedForSite(_ context.Context, _ string) ([]byte, error) {
	f.n++
	return append([]byte(nil), f.key...), nil
}

type fakeFiller struct {
	n      int
	output []byte
	fail   bool
}

func (f *fakeFiller) FillVerifiedTOTP(_ context.Context, _ string, code []byte) error {
	f.n++
	f.output = append([]byte(nil), code...)
	if f.fail {
		return errors.New("fake browser secret")
	}
	return nil
}
func challenge(f Factor) Challenge {
	c := Challenge{Site: "example.com", RequestID: "request-0001", Factor: f, ExpiresAt: testTime.Add(time.Minute)}
	if f == BrokerTOTP {
		c.ParentRequestID = "parent-0001"
	}
	return c
}

func TestFulfillRejectionBeforeSeed(t *testing.T) {
	a := &fakeAuthority{deny: true}
	s := &fakeSeeds{key: []byte("12345678901234567890")}
	f := &fakeFiller{}
	status, err := Fulfill(context.Background(), testTime, challenge(BrokerTOTP), a, s, f)
	if status != "" || !errors.Is(err, ErrDenied) || s.n != 0 || f.n != 0 {
		t.Fatalf("deny bypass: %q %v", status, err)
	}
	if err != nil && bytes.Contains([]byte(err.Error()), []byte("fake secret")) {
		t.Fatal("upstream error leaked")
	}
	a.deny = false
	c := challenge(BrokerTOTP)
	c.Site = "example.com.evil.test"
	// Bad site format alone cannot establish origin authority: verifier must reject.
	a.deny = true
	_, err = Fulfill(context.Background(), testTime, c, a, s, f)
	if !errors.Is(err, ErrDenied) || s.n != 0 {
		t.Fatal("wrong host should not get seed")
	}
	if status, err = Fulfill(context.Background(), testTime, challenge(BrokerTOTP), nil, s, f); !errors.Is(err, ErrDenied) || status != "" {
		t.Fatal("nil authority accepted")
	}
}

func TestManualSecondFactorsNeverFetchSeed(t *testing.T) {
	a := &fakeAuthority{}
	s := &fakeSeeds{}
	f := &fakeFiller{}
	for _, factor := range []Factor{AppleTOTP, Passkey, SMS, Email, Push, RecoveryCode} {
		status, err := Fulfill(context.Background(), testTime, challenge(factor), a, s, f)
		if status != HumanNeeded || err != nil {
			t.Fatalf("factor %q: %v %v", factor, status, err)
		}
	}
	if s.n != 0 || f.n != 0 {
		t.Fatal("manual factor leaked to automation")
	}
}
func TestBrokerTOTPOnlyAfterReservation(t *testing.T) {
	a := &fakeAuthority{}
	s := &fakeSeeds{key: []byte("12345678901234567890")}
	f := &fakeFiller{}
	status, err := Fulfill(context.Background(), testTime, challenge(BrokerTOTP), a, s, f)
	if err != nil || status != Filled || a.n != 1 || s.n != 1 || f.n != 1 || len(f.output) != 6 {
		t.Fatalf("outcome: %v %v", status, err)
	}
	if bytes.Contains([]byte(status), f.output) {
		t.Fatal("OTP leaked in status")
	}
	f.fail = true
	status, err = Fulfill(context.Background(), testTime, challenge(BrokerTOTP), a, s, f)
	if !errors.Is(err, ErrDenied) || status != "" {
		t.Fatal("filler failure not sanitized")
	}
}
func TestTimeOriginFactorAndFailures(t *testing.T) {
	a := &fakeAuthority{}
	s := &fakeSeeds{key: []byte("12345678901234567890")}
	f := &fakeFiller{}
	cases := []Challenge{
		{Site: "EXAMPLE.com", RequestID: "request-0001", Factor: BrokerTOTP, ExpiresAt: testTime.Add(time.Minute)},
		{Site: "example.com", RequestID: "x", Factor: BrokerTOTP, ExpiresAt: testTime.Add(time.Minute)},
		{Site: "example.com", RequestID: "request-0001", Factor: "webauthn_bypass", ExpiresAt: testTime.Add(time.Minute)},
		{Site: "example.com", RequestID: "request-0001", Factor: BrokerTOTP, ExpiresAt: testTime.Add(3 * time.Minute)},
		{Site: "example.com", RequestID: "request-0001", Factor: BrokerTOTP, ExpiresAt: testTime},
	}
	for i, c := range cases {
		if _, err := Fulfill(context.Background(), testTime, c, a, s, f); !errors.Is(err, ErrDenied) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
	if a.n != 0 || s.n != 0 || f.n != 0 {
		t.Fatal("invalid input reached owner authority")
	}
	later := time.Unix(testTime.Unix()+24, 0)
	_, err := Fulfill(context.Background(), later, challenge(BrokerTOTP), a, s, f)
	if !errors.Is(err, ErrCodeWindow) || s.n != 0 || f.n != 0 {
		t.Fatalf("should refuse near-expiry OTP: %v", err)
	}
}
