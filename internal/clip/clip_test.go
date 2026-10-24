package clip_test

import (
	"testing"

	"github.com/camem2442/keycli/internal/clip"
)

func TestChecksumStable(t *testing.T) {
	a := clip.Checksum("hello")
	b := clip.Checksum("hello")
	if a != b {
		t.Fatal(" unstable")
	}
	if a == clip.Checksum("world") {
		t.Fatal("collision")
	}
}
