package clip

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	atotto "github.com/atotto/clipboard"
)

// Provider copies and reads clipboard without logging secret values.
type Provider struct {
	OSC52Enabled bool
}

// AutoClearDelay returns cp auto-clear duration (KC_CP_CLEAR_SECONDS or 30s).
func AutoClearDelay() time.Duration {
	if v := os.Getenv("KC_CP_CLEAR_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 30 * time.Second
}

// Write puts text on the system clipboard (best effort chain).
func (p Provider) Write(text string) error {
	if err := p.writePlatform(text); err == nil {
		return nil
	}
	if p.OSC52Enabled || os.Getenv("KC_CLIP_OSC52") == "1" {
		if err := WriteOSC52(os.Stdout, text); err == nil {
			return nil
		}
	}
	return atotto.WriteAll(text)
}

func (p Provider) writePlatform(text string) error {
	if cmd := toolPath("KC_CLIP_PBCOPY", "pbcopy"); cmd != "" && (runtime.GOOS == "darwin" || os.Getenv("KC_CLIP_PBCOPY") != "") {
		c := exec.Command(cmd)
		c.Stdin = strings.NewReader(text)
		return c.Run()
	}
	if cmd := toolPath("KC_CLIP_WL_COPY", "wl-copy"); cmd != "" && (runtime.GOOS == "linux" || os.Getenv("KC_CLIP_WL_COPY") != "") {
		c := exec.Command(cmd, "--")
		c.Stdin = strings.NewReader(text)
		return c.Run()
	}
	if cmd := toolPath("KC_CLIP_XCLIP", "xclip"); cmd != "" && (runtime.GOOS == "linux" || os.Getenv("KC_CLIP_XCLIP") != "") {
		c := exec.Command(cmd, "-selection", "clipboard")
		c.Stdin = strings.NewReader(text)
		return c.Run()
	}
	return fmt.Errorf("no platform clipboard tool")
}

// Read returns clipboard text.
func (p Provider) Read() (string, error) {
	if cmd := toolPath("KC_CLIP_PBPASTE", "pbpaste"); cmd != "" && (runtime.GOOS == "darwin" || os.Getenv("KC_CLIP_PBPASTE") != "") {
		out, err := exec.Command(cmd).Output()
		return string(out), err
	}
	if cmd := toolPath("KC_CLIP_WL_PASTE", "wl-paste"); cmd != "" && (runtime.GOOS == "linux" || os.Getenv("KC_CLIP_WL_PASTE") != "") {
		out, err := exec.Command(cmd, "--", "-n").Output()
		return string(out), err
	}
	if cmd := toolPath("KC_CLIP_XCLIP", "xclip"); cmd != "" && (runtime.GOOS == "linux" || os.Getenv("KC_CLIP_XCLIP") != "") {
		out, err := exec.Command(cmd, "-selection", "clipboard", "-o").Output()
		return string(out), err
	}
	return atotto.ReadAll()
}

func toolPath(envKey, defaultName string) string {
	if p := os.Getenv(envKey); p != "" {
		return p
	}
	if _, err := exec.LookPath(defaultName); err == nil {
		return defaultName
	}
	return ""
}

// Checksum fingerprints clipboard contents for timed clear.
func Checksum(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Clear attempts to empty the clipboard.
func (p Provider) Clear() error {
	return p.Write("")
}

// CopyWithAutoClear writes text and clears after delay if checksum unchanged.
func (p Provider) CopyWithAutoClear(ctx context.Context, text string, delay time.Duration) error {
	sum := Checksum(text)
	if err := p.Write(text); err != nil {
		return err
	}
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		cur, err := p.Read()
		if err != nil {
			return
		}
		if Checksum(cur) == sum {
			_ = p.Clear()
		}
	}()
	return nil
}

// EncodeOSC52 returns the OSC 52 sequence payload (before tmux wrapping).
func EncodeOSC52(text string) string {
	return base64.StdEncoding.EncodeToString([]byte(text))
}

// SequenceOSC52 builds the terminal OSC 52 escape sequence.
func SequenceOSC52(text string) string {
	return fmt.Sprintf("\x1b]52;c;%s\x07", EncodeOSC52(text))
}

// TmuxPassthrough wraps an OSC sequence for tmux passthrough (see tmux set-clipboard).
func TmuxPassthrough(seq string) string {
	return "\033Ptmux;\033" + strings.TrimPrefix(seq, "\x1b") + "\033\\"
}

// WriteOSC52 emits OSC52 to w (optionally tmux-wrapped).
func WriteOSC52(w io.Writer, text string) error {
	seq := SequenceOSC52(text)
	if os.Getenv("KC_CLIP_OSC52_TMUX") == "1" {
		seq = TmuxPassthrough(seq)
	}
	_, err := io.Copy(w, bytes.NewBufferString(seq))
	return err
}
