package clip

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	atotto "github.com/atotto/clipboard"
)

// Provider copies and reads clipboard without logging secret values.
type Provider struct {
	OSC52Enabled bool
}

// Write puts text on the system clipboard (best effort chain).
func (p Provider) Write(text string) error {
	if err := p.writePlatform(text); err == nil {
		return nil
	}
	if p.OSC52Enabled || os.Getenv("KC_CLIP_OSC52") == "1" {
		if err := writeOSC52(text); err == nil {
			return nil
		}
	}
	return atotto.WriteAll(text)
}

func (p Provider) writePlatform(text string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd := exec.Command("wl-copy", "--")
			cmd.Stdin = strings.NewReader(text)
			return cmd.Run()
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd := exec.Command("xclip", "-selection", "clipboard")
			cmd.Stdin = strings.NewReader(text)
			return cmd.Run()
		}
		return fmt.Errorf("no wl-copy/xclip")
	default:
		return fmt.Errorf("unsupported GOOS")
	}
}

// Read returns clipboard text.
func (p Provider) Read() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		out, err := exec.Command("pbpaste").Output()
		return string(out), err
	case "linux":
		if _, err := exec.LookPath("wl-paste"); err == nil {
			out, err := exec.Command("wl-paste", "--", "-n").Output()
			return string(out), err
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			out, err := exec.Command("xclip", "-selection", "clipboard", "-o").Output()
			return string(out), err
		}
	}
	return atotto.ReadAll()
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

func writeOSC52(text string) error {
	// OSC 52 base64 payload for terminal clipboard (tmux pass-through compatible).
	encoded := base64Encode([]byte(text))
	seq := fmt.Sprintf("\x1b]52;c;%s\x07", encoded)
	_, err := io.Copy(os.Stdout, bytes.NewBufferString(seq))
	return err
}

func base64Encode(b []byte) string {
	const table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out strings.Builder
	for i := 0; i < len(b); i += 3 {
		var n uint32
		remain := len(b) - i
		if remain >= 3 {
			n = uint32(b[i])<<16 | uint32(b[i+1])<<8 | uint32(b[i+2])
			out.WriteByte(table[n>>18&63])
			out.WriteByte(table[n>>12&63])
			out.WriteByte(table[n>>6&63])
			out.WriteByte(table[n&63])
		} else {
			switch remain {
			case 1:
				n = uint32(b[i]) << 16
				out.WriteByte(table[n>>18&63])
				out.WriteByte(table[n>>12&63])
				out.WriteString("==")
			case 2:
				n = uint32(b[i])<<16 | uint32(b[i+1])<<8
				out.WriteByte(table[n>>18&63])
				out.WriteByte(table[n>>12&63])
				out.WriteByte(table[n>>6&63])
				out.WriteString("=")
			}
		}
	}
	return out.String()
}
