package cli

import (
	"strings"
	"testing"
)

func TestOverwriteCandidates(t *testing.T) {
	secrets := map[string][]byte{"a": nil, "b": nil, "c": nil}
	existing := map[string]bool{"a": true, "c": true}
	upper := func(s string) string { return strings.ToUpper(s) }

	got := overwriteCandidates(secrets, existing, upper)
	if len(got) != 2 || got[0] != "A" || got[1] != "C" {
		t.Fatalf("expected [A C], got %v", got)
	}
}

func TestOverwriteCandidatesResolvesAlias(t *testing.T) {
	secrets := map[string][]byte{"gh_alias": nil}
	existing := map[string]bool{"github": true}
	canon := func(s string) string {
		if s == "gh_alias" {
			return "github"
		}
		return s
	}

	got := overwriteCandidates(secrets, existing, canon)
	if len(got) != 1 || got[0] != "github" {
		t.Fatalf("alias must resolve before the existence check, got %v", got)
	}
}

func TestOverwriteCandidatesNone(t *testing.T) {
	secrets := map[string][]byte{"new": nil}
	if got := overwriteCandidates(secrets, map[string]bool{}, func(s string) string { return s }); len(got) != 0 {
		t.Fatalf("expected no overwrites, got %v", got)
	}
}
