package effectclient

import (
	"strings"
	"testing"
)

func TestReadErrorCode(t *testing.T) {
	for _, test := range []struct {
		body string
		code string
	}{
		{body: `{"error":"MUTATION_CONFLICT"}`, code: "MUTATION_CONFLICT"},
		{body: `{"error":"UNRECOGNIZED"}`},
		{body: `{"error":"STALE_GENERATION","unexpected":"text"}`},
		{body: `{"error":"STALE_GENERATION"} {}`},
		{body: `{"error":"STALE_GENERATION"}` + strings.Repeat(" ", 1024)},
	} {
		if code := readErrorCode(strings.NewReader(test.body)); code != test.code {
			t.Errorf("readErrorCode returned %q, want %q", code, test.code)
		}
	}
}
