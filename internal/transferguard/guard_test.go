package transferguard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestReadVerified(t *testing.T) {
	p := []byte("synthetic-only")
	h := sha256.Sum256(p)
	digest := hex.EncodeToString(h[:])
	if _, err := ReadVerified(bytes.NewReader(p), digest); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"bad", strings.Repeat("0", 64)} {
		if _, err := ReadVerified(bytes.NewReader(p), d); err == nil {
			t.Fatal("expected reject")
		}
	}
	if _, err := ReadVerified(bytes.NewReader(make([]byte, MaxBundleBytes+1)), digest); err == nil {
		t.Fatal("oversize accepted")
	}
}
func TestCheckRecipient(t *testing.T) {
	if CheckRecipient("age1synthetic", "age1synthetic") != nil {
		t.Fatal("same rejected")
	}
	for _, v := range []string{"", "age1other"} {
		if CheckRecipient("age1synthetic", v) == nil {
			t.Fatal("mismatch accepted")
		}
	}
}
func TestSingleExpected(t *testing.T) {
	if _, e := SingleExpected(map[string][]byte{"demo": []byte("x")}, "demo"); e != nil {
		t.Fatal(e)
	}
	for _, m := range []map[string][]byte{{}, {"other": []byte("x")}, {"demo": []byte("x"), "other": []byte("x")}} {
		if _, e := SingleExpected(m, "demo"); e == nil {
			t.Fatal("invalid bundle accepted")
		}
	}
}
