package keychain

import (
	"testing"

	"github.com/camem2442/keycli/internal/kcerrors"
)

func TestValidateSecretName(t *testing.T) {
	valid := []string{"demo", "foo.bar", "GITHUB_TOKEN", "groq#3", "a:b", "a-b", "kc.secret.foo"}
	for _, name := range valid {
		if err := ValidateSecretName(name); err != nil {
			t.Fatalf("ValidateSecretName(%q) = %v", name, err)
		}
	}
	invalid := []string{"", " bad", "bad name", "bad\nname", "bad\tname", "bad/name", `.hidden`, "-hidden", `bad"name`, `bad\name`, "kc.age", "KC.AGE"}
	for _, name := range invalid {
		if err := ValidateSecretName(name); err == nil {
			t.Fatalf("ValidateSecretName(%q) accepted", name)
		} else if kcerrors.Code(err) != kcerrors.ExitUsage {
			t.Fatalf("ValidateSecretName(%q) code = %d", name, kcerrors.Code(err))
		}
	}
}

func TestWriteServiceForcesPrefix(t *testing.T) {
	k := &Keychain{}
	cases := map[string]string{
		"demo":          "kc.secret.demo",
		"foo.bar":       "kc.secret.foo.bar",
		"kc.secret.foo": "kc.secret.foo",
	}
	for name, want := range cases {
		got, err := k.writeService(name)
		if err != nil || got != want {
			t.Fatalf("writeService(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := k.writeService("kc.age"); kcerrors.Code(err) != kcerrors.ExitUsage {
		t.Fatalf("kc.age must be reserved: %v", err)
	}
	if _, err := k.writeService("kc.secret."); kcerrors.Code(err) != kcerrors.ExitUsage {
		t.Fatalf("empty prefixed name must be rejected: %v", err)
	}
}
