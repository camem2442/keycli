package agebundle_test

import (
	"bytes"
	"testing"

	"filippo.io/age"
	"github.com/camem2442/keycli/internal/agebundle"
)

func TestRoundTrip(t *testing.T) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec := id.Recipient()
	var buf bytes.Buffer
	secrets := map[string][]byte{"github": []byte("tok")}
	if err := agebundle.Encrypt(&buf, secrets, []age.Recipient{rec}); err != nil {
		t.Fatal(err)
	}
	out, err := agebundle.Decrypt(&buf, id)
	if err != nil {
		t.Fatal(err)
	}
	if string(out["github"]) != "tok" {
		t.Fatalf("got %q", out["github"])
	}
}
