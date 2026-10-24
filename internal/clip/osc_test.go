package clip_test

import (
	"bytes"
	"testing"

	"github.com/camem2442/keycli/internal/clip"
)

func TestOSC52Golden(t *testing.T) {
	seq := clip.SequenceOSC52("hi")
	want := "\x1b]52;c;aGk=\x07"
	if seq != want {
		t.Fatalf("got %q want %q", seq, want)
	}
	tmux := clip.TmuxPassthrough(seq)
	if !bytes.Contains([]byte(tmux), []byte("tmux")) {
		t.Fatal("missing tmux wrapper")
	}
}
