package interactive

import (
	"bytes"
	"testing"

	"github.com/camem2442/keycli/internal/resolve"
)

func TestFilterItems(t *testing.T) {
	items := []resolve.ListItem{
		{Name: "openai_api_key", Aliases: []string{"oai"}, Note: "work api key"},
		{Name: "github", Aliases: []string{"gh", "gh_token"}, Note: "personal token"},
		{Name: "aws_secret_key", Note: "infra credentials"},
	}

	// Empty query returns all
	if got := FilterItems(items, ""); len(got) != 3 {
		t.Fatalf("expected 3 items, got %d", len(got))
	}

	// Substring on name
	if got := FilterItems(items, "open"); len(got) != 1 || got[0].Name != "openai_api_key" {
		t.Fatalf("unexpected filter by name: %v", got)
	}

	// Substring on alias
	if got := FilterItems(items, "gh_token"); len(got) != 1 || got[0].Name != "github" {
		t.Fatalf("unexpected filter by alias: %v", got)
	}

	// Substring on note
	if got := FilterItems(items, "infra"); len(got) != 1 || got[0].Name != "aws_secret_key" {
		t.Fatalf("unexpected filter by note: %v", got)
	}

	// Case insensitive
	if got := FilterItems(items, "GITHUB"); len(got) != 1 || got[0].Name != "github" {
		t.Fatalf("unexpected case-insensitive filter: %v", got)
	}

	// No match
	if got := FilterItems(items, "nonexistent"); len(got) != 0 {
		t.Fatalf("expected 0 items, got %d", len(got))
	}
}

func TestReadKey(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected Key
		expRune  rune
	}{
		{"Enter LF", []byte{'\n'}, KeyEnter, 0},
		{"Enter CR", []byte{'\r'}, KeyEnter, 0},
		{"Ctrl+C", []byte{3}, KeyCtrlC, 0},
		{"Ctrl+D", []byte{4}, KeyCtrlD, 0},
		{"Backspace 127", []byte{127}, KeyBackspace, 0},
		{"Backspace 8", []byte{8}, KeyBackspace, 0},
		{"Tab", []byte{'\t'}, KeyTab, 0},
		{"Arrow Up", []byte{27, '[', 'A'}, KeyUp, 0},
		{"Arrow Down", []byte{27, '[', 'B'}, KeyDown, 0},
		{"Arrow Right", []byte{27, '[', 'C'}, KeyRight, 0},
		{"Arrow Left", []byte{27, '[', 'D'}, KeyLeft, 0},
		{"Page Up", []byte{27, '[', '5', '~'}, KeyPageUp, 0},
		{"Page Down", []byte{27, '[', '6', '~'}, KeyPageDown, 0},
		{"Home", []byte{27, '[', 'H'}, KeyHome, 0},
		{"End", []byte{27, '[', 'F'}, KeyEnd, 0},
		{"Rune 'a'", []byte{'a'}, KeyRune, 'a'},
		{"Rune 'Z'", []byte{'Z'}, KeyRune, 'Z'},
		{"Rune '?'", []byte{'?'}, KeyRune, '?'},
		{"Escape alone", []byte{27}, KeyEscape, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.input)
			kp, err := ReadKey(r)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if kp.Key != tc.expected {
				t.Errorf("expected key %v, got %v", tc.expected, kp.Key)
			}
			if tc.expRune != 0 && kp.Rune != tc.expRune {
				t.Errorf("expected rune %c, got %c", tc.expRune, kp.Rune)
			}
		})
	}
}

func TestFormattingHelpers(t *testing.T) {
	if got := TruncateString("hello world", 5); got != "he..." {
		t.Errorf("unexpected truncate: %q", got)
	}
	if got := TruncateString("short", 10); got != "short" {
		t.Errorf("unexpected truncate: %q", got)
	}
	if got := PadRight("abc", 6); got != "abc   " {
		t.Errorf("unexpected pad: %q", got)
	}
}
