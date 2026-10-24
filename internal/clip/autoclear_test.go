package clip_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/camem2442/keycli/internal/clip"
)

func TestAutoClearUnchangedOnly(t *testing.T) {
	dir := t.TempDir()
	clipBin := filepath.Join(dir, "fakeclip")
	root, _ := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	out, err := exec.Command("go", "build", "-o", clipBin, filepath.Join(strings.TrimSpace(string(root)), "cmd/fakeclip")).CombinedOutput()
	if err != nil {
		t.Fatal(string(out))
	}
	pbcopy := filepath.Join(dir, "pbcopy")
	pbpaste := filepath.Join(dir, "pbpaste")
	_ = os.Symlink(clipBin, pbcopy)
	_ = os.Symlink(clipBin, pbpaste)
	clipFile := filepath.Join(dir, "clip.txt")
	t.Setenv("KC_CLIP_PBCOPY", pbcopy)
	t.Setenv("KC_CLIP_PBPASTE", pbpaste)
	t.Setenv("KC_FAKE_CLIPBOARD", clipFile)

	p := clip.Provider{}
	text := "clip-secret"
	if err := p.CopyWithAutoClear(context.Background(), text, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if b, _ := os.ReadFile(clipFile); len(b) != 0 {
		t.Fatal("expected cleared")
	}

	_ = p.Write("other")
	if err := p.CopyWithAutoClear(context.Background(), text, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	_ = p.Write("other")
	time.Sleep(80 * time.Millisecond)
	if b, _ := os.ReadFile(clipFile); string(b) != "other" {
		t.Fatal("should not clear when clipboard changed")
	}
}
