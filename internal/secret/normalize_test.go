package secret_test

import (
	"bytes"
	"testing"

	"github.com/camem2442/keycli/internal/secret"
)

func TestNormalizeSecret(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want []byte
	}{
		{"crlf_suffix", []byte("ghp_abc\r\n"), []byte("ghp_abc")},
		{"lf_suffix", []byte("ghp_abc\n"), []byte("ghp_abc")},
		{"leading_trailing_space", []byte("  ghp_abc  "), []byte("ghp_abc")},
		{"embedded_newlines", []byte("ghp\r\n_ab\nc"), []byte("ghp_abc")},
		{"empty", []byte(""), nil},
		{"whitespace_only", []byte(" \r\n\t "), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := secret.NormalizeSecret(tt.in)
			if !bytes.Equal(got, tt.want) {
				t.Fatalf("NormalizeSecret(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
