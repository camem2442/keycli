package applecsv

import (
	"strings"
	"testing"
)

func TestFormatHeaderAndCRLF(t *testing.T) {
	out, err := Format([]Row{{Title: "demo", Password: "synthetic-only"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.HasPrefix(text, "Title,URL,Username,Password,Notes,OTPAuth\r\n") {
		t.Fatalf("header mismatch: %q", text)
	}
	if !strings.Contains(text, "demo,,,synthetic-only,,\r\n") {
		t.Fatalf("row mismatch: %q", text)
	}
}

func TestFormatQuotesSeparatorsQuotesAndNewlines(t *testing.T) {
	out, err := Format([]Row{{
		Title:    `a,b`,
		Username: `say "hi"`,
		Password: "line1\nline2",
		Notes:    "CR\rLF\r\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Parse(strings.NewReader(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("round-trip rows = %d", len(rows))
	}
	got := rows[0]
	if got.Title != "a,b" || got.Username != `say "hi"` || got.Password != "line1\nline2" || got.Notes != "CR\rLF\r\n" {
		t.Fatal("quoted fields did not round-trip")
	}
}

func TestFormatRejectsInvalidUTF8(t *testing.T) {
	_, err := Format([]Row{{Title: "demo", Password: "\xff\xfe"}})
	if err == nil || !strings.Contains(err.Error(), "Password") {
		t.Fatalf("invalid UTF-8 accepted: %v", err)
	}
}

func TestParseAcceptsBOMAndCRLFAndReorderedColumns(t *testing.T) {
	in := "\xEF\xBB\xBFTitle,URL,Username,Password,Notes,OTPAuth\r\n" +
		"site,https://example.test/,user,synthetic-only,note,otpauth://totp?secret=SYNTHETIC&algorithm=SHA1&digits=6&period=30\r\n" +
		",,,,,\r\n"
	rows, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, empty row must be skipped", len(rows))
	}
	if rows[0].Username != "user" || !IsOTPAuth(rows[0].OTPAuth) {
		t.Fatal("columns mapped wrong")
	}
}

func TestParseHeaderMissingColumn(t *testing.T) {
	_, err := Parse(strings.NewReader("Title,URL,Username,Password,Notes\n"))
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("missing column accepted: %v", err)
	}
}

func TestParseWrongFieldCount(t *testing.T) {
	in := "Title,URL,Username,Password,Notes,OTPAuth\nonly,four,fields\n"
	if _, err := Parse(strings.NewReader(in)); err == nil {
		t.Fatal("short row accepted")
	}
}

func TestParseUnterminatedQuote(t *testing.T) {
	in := "Title,URL,Username,Password,Notes,OTPAuth\n\"demo,,,value,,\n"
	_, err := Parse(strings.NewReader(in))
	if err == nil || !strings.Contains(err.Error(), "unterminated") {
		t.Fatalf("unterminated quote accepted: %v", err)
	}
}

func TestParseTooManyColumns(t *testing.T) {
	in := "Title,URL,Username,Password,Notes,OTPAuth\na,b,c,d,e,f,g\n"
	if _, err := Parse(strings.NewReader(in)); err == nil {
		t.Fatal("seven-column record accepted")
	}
}

func TestParseEmptyFile(t *testing.T) {
	if _, err := Parse(strings.NewReader("")); err == nil {
		t.Fatal("empty csv accepted")
	}
}

func TestIsOTPAuthLabelLess(t *testing.T) {
	for _, v := range []string{
		"otpauth://totp?secret=AAAAAAAA&algorithm=SHA1&digits=6&period=30",
		"otpauth://totp/Example:me?secret=AAAAAAAA",
		"  OTPAUTH://TOTP?secret=AAAAAAAA",
	} {
		if !IsOTPAuth(v) {
			t.Fatalf("rejected %q", v)
		}
	}
	if IsOTPAuth("JBSWY3DPEHPK3PXP") {
		t.Fatal("bare base32 seed treated as a URI")
	}
}

func TestNameFromTitle(t *testing.T) {
	cases := map[string]string{
		"GitHub":                    "github",
		"  My Bank – 개인 계정  ":       "my-bank-개인-계정",
		"a/b c":                     "a-b-c",
		"---":                       "apple-entry",
		"":                          "apple-entry",
		"owner-camem2442-gitcoord":  "owner-camem2442-gitcoord",
		"Name.With.Dots_and-dashes": "name.with.dots_and-dashes",
	}
	for in, want := range cases {
		if got := NameFromTitle(in); got != want {
			t.Errorf("NameFromTitle(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("x", 200)
	if got := NameFromTitle(long); len(got) > maxNameBytes {
		t.Fatalf("name not truncated: %d bytes", len(got))
	}
}

func TestMalformedInputDoesNotEchoCell(t *testing.T) {
	const marker = "synthetic-private-cell"
	_, err := Parse(strings.NewReader("Title,URL,Username," + marker + ",Notes,OTPAuth\r\n"))
	if err == nil || strings.Contains(err.Error(), marker) {
		t.Fatal("expected a redacted error for mistaken header contents")
	}
}

func TestParseKeepsEmptyLastCellWithoutTerminalNewline(t *testing.T) {
	rows, err := Parse(strings.NewReader("Title,URL,Username,Password,Notes,OTPAuth\r\nFixture,,,synthetic,,"))
	if err != nil || len(rows) != 1 || rows[0].OTPAuth != "" {
		t.Fatal("expected a full record with empty final cell")
	}
}
