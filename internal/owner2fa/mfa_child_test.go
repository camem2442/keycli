package owner2fa

import (
	"context"
	"errors"
	"testing"
	"time"
)

type childOriginal struct {
	calls  int
	reject bool
}

func (o *childOriginal) VerifyAndReserve(_ context.Context, _ Challenge) error {
	o.calls++
	if o.reject {
		return errors.New("secret canary")
	}
	return nil
}

type childSeed struct{ calls int }

func (s *childSeed) SeedForSite(_ context.Context, _ string) ([]byte, error) {
	s.calls++
	return []byte("12345678901234567890"), nil
}

type childFill struct{ calls int }

func (f *childFill) FillVerifiedTOTP(_ context.Context, _ string, _ []byte) error {
	f.calls++
	return nil
}
func childFixture() Challenge {
	return Challenge{Site: "example.com", ParentRequestID: "parent-0001", RequestID: "child-0001", Factor: BrokerTOTP, ExpiresAt: time.Unix(1795172431, 0).Add(time.Minute)}
}
func TestBoundMFAChildRejectsParentReuseBeforeSeed(t *testing.T) {
	orig := &childOriginal{}
	child := &BoundMFAChild{ParentRequestID: "parent-0001", ChildRequestID: "child-0001", Site: "example.com", Inner: orig}
	c := childFixture()
	c.RequestID = c.ParentRequestID
	s := &childSeed{}
	f := &childFill{}
	_, err := Fulfill(context.Background(), time.Unix(1795172431, 0), c, child, s, f)
	if !errors.Is(err, ErrDenied) || orig.calls != 0 || s.calls != 0 || f.calls != 0 {
		t.Fatalf("parent replay escaped: %v", err)
	}
}
func TestBoundMFAChildMismatchAndOriginalGate(t *testing.T) {
	orig := &childOriginal{}
	child := &BoundMFAChild{ParentRequestID: "parent-0001", ChildRequestID: "child-0001", Site: "example.com", Inner: orig}
	c := childFixture()
	c.ParentRequestID = "not-bound"
	if err := child.VerifyAndReserve(context.Background(), c); !errors.Is(err, ErrDenied) {
		t.Fatal("parent mismatch")
	}
	c = childFixture()
	c.Site = "evil.test"
	if err := child.VerifyAndReserve(context.Background(), c); !errors.Is(err, ErrDenied) {
		t.Fatal("site mismatch")
	}
	c = childFixture()
	orig.reject = true
	if err := child.VerifyAndReserve(context.Background(), c); !errors.Is(err, ErrDenied) {
		t.Fatal("original rejection not enforced")
	}
	if orig.calls != 1 {
		t.Fatal("forged child reached original authority")
	}
}
func TestBoundMFAChildSuccessStillRequiresOriginalAuthority(t *testing.T) {
	orig := &childOriginal{}
	child := &BoundMFAChild{ParentRequestID: "parent-0001", ChildRequestID: "child-0001", Site: "example.com", Inner: orig}
	seed := &childSeed{}
	fill := &childFill{}
	now := time.Unix(1795172431, 0)
	status, err := Fulfill(context.Background(), now, childFixture(), child, seed, fill)
	if err != nil || status != Filled || orig.calls != 1 || seed.calls != 1 || fill.calls != 1 {
		t.Fatalf("child not bound: %v %v", status, err)
	}
}
func TestUnboundBrokerChildDenied(t *testing.T) {
	now := time.Unix(1795172431, 0)
	orig := &childOriginal{}
	seed := &childSeed{}
	fill := &childFill{}
	c := childFixture()
	c.ParentRequestID = ""
	status, err := Fulfill(context.Background(), now, c, orig, seed, fill)
	if !errors.Is(err, ErrDenied) || status != "" || orig.calls != 0 || seed.calls != 0 {
		t.Fatal("missing parent escaped")
	}
}
