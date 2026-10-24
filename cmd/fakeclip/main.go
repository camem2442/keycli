// Command fakeclip is symlinked as pbcopy, pbpaste, wl-copy, or wl-paste in tests.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func main() {
	cmd := filepath.Base(os.Args[0])
	if cmd == "fakeclip" && len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "pbcopy", "wl-copy":
		data, _ := io.ReadAll(os.Stdin)
		_ = os.WriteFile(stateFile(), data, 0o600)
	case "pbpaste", "wl-paste":
		data, _ := os.ReadFile(stateFile())
		os.Stdout.Write(data)
	default:
		fmt.Fprintln(os.Stderr, "unknown:", cmd)
		os.Exit(1)
	}
}

func stateFile() string {
	if p := os.Getenv("KC_FAKE_CLIPBOARD"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "kc-fake-clipboard.txt")
}
