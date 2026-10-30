package interactive

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

// Key represents a parsed key stroke or event.
type Key int

const (
	KeyUnknown Key = iota
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyEnter
	KeyEscape
	KeyBackspace
	KeyDelete
	KeyTab
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyRune
	KeyCtrlC
	KeyCtrlD
)

// KeyPress contains the key type and rune if KeyRune.
type KeyPress struct {
	Key  Key
	Rune rune
}

// IsTerminal checks if the given file is an interactive terminal.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// ReadKey reads a single keypress from a terminal in raw mode.
func ReadKey(reader io.Reader) (KeyPress, error) {
	var buf [16]byte
	n, err := reader.Read(buf[:])
	if err != nil {
		return KeyPress{Key: KeyUnknown}, err
	}
	if n == 0 {
		return KeyPress{Key: KeyUnknown}, nil
	}

	b := buf[0]

	// Handle Control characters
	if b == 3 { // Ctrl+C
		return KeyPress{Key: KeyCtrlC}, nil
	}
	if b == 4 { // Ctrl+D
		return KeyPress{Key: KeyCtrlD}, nil
	}
	if b == 13 || b == 10 { // Enter / Return
		return KeyPress{Key: KeyEnter}, nil
	}
	if b == 9 { // Tab
		return KeyPress{Key: KeyTab}, nil
	}
	if b == 127 || b == 8 { // Backspace
		return KeyPress{Key: KeyBackspace}, nil
	}

	// Escape sequences
	if b == 27 {
		if n == 1 {
			return KeyPress{Key: KeyEscape}, nil
		}
		if n >= 3 && buf[1] == '[' {
			switch buf[2] {
			case 'A':
				return KeyPress{Key: KeyUp}, nil
			case 'B':
				return KeyPress{Key: KeyDown}, nil
			case 'C':
				return KeyPress{Key: KeyRight}, nil
			case 'D':
				return KeyPress{Key: KeyLeft}, nil
			case 'H':
				return KeyPress{Key: KeyHome}, nil
			case 'F':
				return KeyPress{Key: KeyEnd}, nil
			case '3':
				if n >= 4 && buf[3] == '~' {
					return KeyPress{Key: KeyDelete}, nil
				}
			case '5':
				if n >= 4 && buf[3] == '~' {
					return KeyPress{Key: KeyPageUp}, nil
				}
			case '6':
				if n >= 4 && buf[3] == '~' {
					return KeyPress{Key: KeyPageDown}, nil
				}
			}
		} else if n >= 3 && buf[1] == 'O' {
			switch buf[2] {
			case 'H':
				return KeyPress{Key: KeyHome}, nil
			case 'F':
				return KeyPress{Key: KeyEnd}, nil
			}
		}
		return KeyPress{Key: KeyEscape}, nil
	}

	// Normal printable character
	r := rune(b)
	return KeyPress{Key: KeyRune, Rune: r}, nil
}

// ANSI styling constants
const (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Underline = "\033[4m"
	Inverse   = "\033[7m"

	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	Gray    = "\033[90m"

	BgCyan  = "\033[46m"
	BgBlue  = "\033[44m"
	BgGray  = "\033[100m"

	ClearLine = "\033[2K\r"
	HideCursor = "\033[?25l"
	ShowCursor = "\033[?25h"
)

// TruncateString truncates a string to maxLen runes with ellipsis.
func TruncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// PadRight pads s with spaces to target width.
func PadRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return s
	}
	return s + fmt.Sprintf("%*s", width-len(r), "")
}
