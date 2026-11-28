// Package applecsv reads and writes the CSV format that the Apple Passwords
// app imports and exports.
//
// The file is plain text with six columns:
//
//	Title,URL,Username,Password,Notes,OTPAuth
//
// Apple writes every column on every row, uses an empty cell for a field the
// person never filled in, and canonicalises verification codes as a label-less
// URI ("otpauth://totp?secret=…&algorithm=SHA1&digits=6&period=30"). Readers
// that expect "otpauth://totp/<label>" reject those rows, so IsOTPAuth accepts
// both spellings.
//
// The codec is deliberately local rather than encoding/csv: the stdlib reader
// rewrites a CRLF inside a quoted field to LF and the stdlib writer with CRLF
// output drops a bare CR, and both change the bytes of a stored secret.
//
// Nothing here logs, prints or hashes values. Errors carry record numbers and
// column names only.
package applecsv

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Header is the column order Apple writes and expects.
var Header = []string{"Title", "URL", "Username", "Password", "Notes", "OTPAuth"}

const (
	// MaxRows bounds how many records an import will read.
	MaxRows = 20000
	// MaxInputBytes bounds the size of a CSV read from a file or stdin.
	MaxInputBytes = 32 << 20
	// maxFieldBytes bounds one decoded cell.
	maxFieldBytes = 1 << 20
	// maxNameBytes bounds a generated secret name.
	maxNameBytes = 64
)

// Row is one Apple Passwords entry.
type Row struct {
	Title    string
	URL      string
	Username string
	Password string
	Notes    string
	OTPAuth  string
}

// Format renders rows as the CSV Apple Passwords imports. The header is always
// written and records end with CRLF, as RFC 4180 requires.
func Format(rows []Row) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(EncodeRecord(Header))
	for i, r := range rows {
		fields := []string{r.Title, r.URL, r.Username, r.Password, r.Notes, r.OTPAuth}
		for j, f := range fields {
			if !utf8.ValidString(f) {
				return nil, fmt.Errorf("row %d (%s): invalid UTF-8", i+2, Header[j])
			}
		}
		buf.WriteString(EncodeRecord(fields))
	}
	return buf.Bytes(), nil
}

// EncodeRecord renders one record, quoting a field only when it holds a
// comma, quote, CR or LF.
func EncodeRecord(fields []string) string {
	var b strings.Builder
	for i, f := range fields {
		if i > 0 {
			b.WriteByte(',')
		}
		if !strings.ContainsAny(f, ",\"\r\n") {
			b.WriteString(f)
			continue
		}
		b.WriteByte('"')
		b.WriteString(strings.ReplaceAll(f, `"`, `""`))
		b.WriteByte('"')
	}
	b.WriteString("\r\n")
	return b.String()
}

// Parse reads a CSV produced by the Apple Passwords app or any file with the
// same six columns. Column order may differ from Apple's, but all six names
// must be present. Records made only of empty cells are skipped.
func Parse(r io.Reader) ([]Row, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxInputBytes {
		return nil, fmt.Errorf("csv larger than %d bytes", MaxInputBytes)
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	p := &parser{data: data, record: 1}
	head, err := p.next()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("csv is empty; expected header %s", strings.Join(Header, ","))
		}
		return nil, fmt.Errorf("csv header: %w", err)
	}
	index, err := headerIndex(head)
	if err != nil {
		return nil, err
	}

	var rows []Row
	for {
		rec, err := p.next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(rows) >= MaxRows {
			return nil, fmt.Errorf("csv has more than %d rows", MaxRows)
		}
		row := Row{
			Title:    rec[index["Title"]],
			URL:      rec[index["URL"]],
			Username: rec[index["Username"]],
			Password: rec[index["Password"]],
			Notes:    rec[index["Notes"]],
			OTPAuth:  rec[index["OTPAuth"]],
		}
		if row == (Row{}) {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// IsOTPAuth reports whether a value is an otpauth URI, including Apple's
// label-less spelling.
func IsOTPAuth(value string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "otpauth://")
}

// NameFromTitle derives a secret name from an Apple entry title: lower case,
// letters, digits, '.', '_' and '-' kept, every other rune collapsed to '-'.
func NameFromTitle(title string) string {
	var b strings.Builder
	dashed := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '.', r == '_':
			b.WriteRune(r)
			dashed = false
		default:
			if !dashed && b.Len() > 0 {
				b.WriteByte('-')
				dashed = true
			}
		}
	}
	name := strings.Trim(b.String(), "-._")
	if name == "" {
		return "apple-entry"
	}
	return truncate(name, maxNameBytes)
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := 0
	for i := range s {
		if i > limit {
			break
		}
		cut = i
	}
	return strings.TrimRight(s[:cut], "-._")
}

func headerIndex(head []string) (map[string]int, error) {
	if len(head) != len(Header) {
		return nil, fmt.Errorf("csv header has %d columns; expected %s",
			len(head), strings.Join(Header, ","))
	}
	index := make(map[string]int, len(Header))
	for i, col := range head {
		name := canonicalHeader(col)
		if name == "" {
			return nil, fmt.Errorf("csv header column %d is unrecognized; expected one of %s",
				i+1, strings.Join(Header, ","))
		}
		if _, dup := index[name]; dup {
			return nil, fmt.Errorf("csv header repeats %q", name)
		}
		index[name] = i
	}
	return index, nil
}

func canonicalHeader(col string) string {
	for _, want := range Header {
		if strings.EqualFold(strings.TrimSpace(col), want) {
			return want
		}
	}
	return ""
}

// parser is a byte-preserving CSV reader: a CRLF inside a quoted field stays
// CRLF, and only record terminators are consumed.
type parser struct {
	data   []byte
	pos    int
	record int
}

func (p *parser) next() ([]string, error) {
	if p.pos >= len(p.data) {
		return nil, io.EOF
	}
	fields := make([]string, 0, len(Header))
	for {
		field, err := p.field()
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
		if p.pos >= len(p.data) {
			break
		}
		switch p.data[p.pos] {
		case ',':
			p.pos++
		case '\r':
			p.pos++
			if p.pos < len(p.data) && p.data[p.pos] == '\n' {
				p.pos++
			}
			return p.recordDone(fields)
		case '\n':
			p.pos++
			return p.recordDone(fields)
		default:
			return nil, fmt.Errorf("record %d: unexpected data at offset %d after a quoted field",
				p.record, p.pos)
		}
	}
	return p.recordDone(fields)
}

func (p *parser) recordDone(fields []string) ([]string, error) {
	p.record++
	if len(fields) != len(Header) {
		return nil, fmt.Errorf("record %d has %d columns; expected %d",
			p.record-1, len(fields), len(Header))
	}
	return fields, nil
}

func (p *parser) field() (string, error) {
	if p.pos < len(p.data) && p.data[p.pos] == '"' {
		return p.quoted()
	}
	start := p.pos
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ',', '\r', '\n':
			return p.checkField(string(p.data[start:p.pos]))
		}
		p.pos++
	}
	return p.checkField(string(p.data[start:p.pos]))
}

func (p *parser) quoted() (string, error) {
	p.pos++ // opening quote
	var b strings.Builder
	for p.pos < len(p.data) {
		c := p.data[p.pos]
		if c != '"' {
			b.WriteByte(c)
			p.pos++
			if b.Len() > maxFieldBytes {
				return "", fmt.Errorf("record %d: field larger than %d bytes", p.record, maxFieldBytes)
			}
			continue
		}
		if p.pos+1 < len(p.data) && p.data[p.pos+1] == '"' {
			b.WriteByte('"')
			p.pos += 2
			continue
		}
		p.pos++ // closing quote
		return b.String(), nil
	}
	return "", fmt.Errorf("record %d: unterminated quoted field", p.record)
}

func (p *parser) checkField(s string) (string, error) {
	if len(s) > maxFieldBytes {
		return "", fmt.Errorf("record %d: field larger than %d bytes", p.record, maxFieldBytes)
	}
	return s, nil
}
