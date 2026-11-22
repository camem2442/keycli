package browserintent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var now = time.Unix(1795172400, 0)

func validJSON() string {
	return `{"v":1,"goal":"goal-0001","claim":"claim-0001","request_id":"request-0001","site":"example.com","action":"session_status","path":"/account","expires_unix":1795172450}`
}
func TestDecodeValid(t *testing.T) {
	got, err := Decode(strings.NewReader(validJSON()), now)
	if err != nil || got.Site != "example.com" {
		t.Fatalf("valid request failed: %v", err)
	}
}
func TestDecodeRejectsSmuggling(t *testing.T) {
	cases := []string{
		strings.Replace(validJSON(), `"site":"example.com"`, `"site":"example.com","site":"evil.test"`, 1),
		strings.Replace(validJSON(), `"site":"example.com"`, `"site":"example.com","password":"secret"`, 1),
		strings.Replace(validJSON(), `"site":"example.com"`, `"site":"*.example.com"`, 1),
		strings.Replace(validJSON(), `"path":"/account"`, `"path":"//evil.test"`, 1),
		strings.Replace(validJSON(), `"path":"/account"`, `"path":"/%2fadmin"`, 1),
		strings.Replace(validJSON(), `"action":"session_status"`, `"action":"execute_js"`, 1),
		strings.Replace(validJSON(), `"expires_unix":1795172450`, `"expires_unix":1795174400`, 1),
		validJSON() + `{}`,
		`{"v":1,"goal":"x"}`,
		strings.Repeat(" ", 4097),
	}
	for i, s := range cases {
		if _, err := Decode(strings.NewReader(s), now); !errors.Is(err, ErrRejected) {
			t.Fatalf("case %d escaped reject: %v", i, err)
		}
	}
}

type permit struct {
	allow  bool
	called int
}

func (p *permit) VerifyAndReserve(_ context.Context, _ Intent) error {
	p.called++
	if !p.allow {
		return errors.New("no grant")
	}
	return nil
}

type worker struct {
	calls  int
	result Outcome
}

func (w *worker) Perform(_ context.Context, _ Intent) (Outcome, error) {
	w.calls++
	return w.result, nil
}
func TestGateCannotBypassAuthority(t *testing.T) {
	i, err := Decode(strings.NewReader(validJSON()), now)
	if err != nil {
		t.Fatal(err)
	}
	w := &worker{result: Performed}
	p := &permit{}
	if _, err := Gate(context.Background(), now, i, p, w); !errors.Is(err, ErrRejected) {
		t.Fatalf("denied grant: %v", err)
	}
	if w.calls != 0 {
		t.Fatal("worker invoked after denied grant")
	}
	if _, err := Gate(context.Background(), now, i, nil, w); !errors.Is(err, ErrRejected) {
		t.Fatalf("nil verifier accepted: %v", err)
	}
	p.allow = true
	if out, err := Gate(context.Background(), now, i, p, w); err != nil || out != Performed || w.calls != 1 {
		t.Fatalf("allowed mock: %v %v", out, err)
	}
	w.result = Outcome("unstructured-secret")
	if _, err := Gate(context.Background(), now, i, p, w); !errors.Is(err, ErrRejected) {
		t.Fatal("unstructured browser output escaped")
	}
}
