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
	w := &worker{result: SessionPresent}
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
	if out, err := Gate(context.Background(), now, i, p, w); err != nil || out != SessionPresent || w.calls != 1 {
		t.Fatalf("allowed mock: %v %v", out, err)
	}
	w.result = Outcome("unstructured-secret")
	if _, err := Gate(context.Background(), now, i, p, w); !errors.Is(err, ErrRejected) {
		t.Fatal("unstructured browser output escaped")
	}
}

func TestGateDoesNotConflateBrowserAckAndSessionState(t *testing.T) {
	makeIntent := func(action string) Intent {
		i, err := Decode(strings.NewReader(validJSON()), now)
		if err != nil {
			t.Fatal(err)
		}
		i.Action = action
		return i
	}
	for _, tc := range []struct {
		action string
		out    Outcome
		accept bool
	}{
		{"session_status", SessionPresent, true},
		{"session_status", NoSession, true},
		{"session_status", NeedsHuman, false},
		{"session_status", Performed, false},
		{"reauthenticate", SessionPresent, true},
		{"reauthenticate", NeedsHuman, true},
		{"reauthenticate", NoSession, false},
		{"reauthenticate", Performed, false},
		{"read_page", SessionPresent, false},
	} {
		t.Run(tc.action+"/"+string(tc.out), func(t *testing.T) {
			p := &permit{allow: true}
			w := &worker{result: tc.out}
			out, err := Gate(context.Background(), now, makeIntent(tc.action), p, w)
			if tc.accept {
				if err != nil || out != tc.out || w.calls != 1 {
					t.Fatalf("valid outcome rejected %q: %v", out, err)
				}
			} else if !errors.Is(err, ErrRejected) || out != "" {
				t.Fatalf("unverified outcome was accepted %q: %v", out, err)
			}
			if tc.action == "read_page" && (p.called != 0 || w.calls != 0) {
				t.Fatal("unsupported read_page triggered an effect")
			}
		})
	}
}

func TestOutcomeFromOwnerStatusIsBoundToAction(t *testing.T) {
	cases := []struct {
		action string
		status string
		want   Outcome
		valid  bool
	}{
		{"session_status", "SESSION_PRESENT", SessionPresent, true},
		{"session_status", "NO_SESSION", NoSession, true},
		{"reauthenticate", "SESSION_PRESENT", SessionPresent, true},
		{"reauthenticate", "HUMAN_ACTION_REQUIRED", NeedsHuman, true},
		{"session_status", "HUMAN_ACTION_REQUIRED", "", false},
		{"reauthenticate", "NO_SESSION", "", false},
		{"read_page", "SESSION_PRESENT", "", false},
		{"session_status", "user@example.com:secret", "", false},
	}
	for _, tc := range cases {
		out, err := OutcomeFromOwnerStatus(tc.action, tc.status)
		if tc.valid {
			if err != nil || out != tc.want {
				t.Fatalf("%+v: %s %v", tc, out, err)
			}
		} else if !errors.Is(err, ErrRejected) || out != "" {
			t.Fatalf("unexpected native status accepted: %+v => %q", tc, out)
		}
	}
}
