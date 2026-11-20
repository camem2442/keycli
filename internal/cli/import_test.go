package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/camem2442/keycli/internal/backend"
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

type fakeAbsentStore struct {
	values map[string]string
}

func (f *fakeAbsentStore) SetIfAbsent(_ context.Context, name string, value []byte, _ backend.Meta) error {
	if _, ok := f.values[name]; ok {
		return errors.New("secret already exists")
	}
	f.values[name] = string(value)
	return nil
}

func TestCreateImportsRefusesConcurrentCreation(t *testing.T) {
	// "late" appears after the pre-flight list, so the atomic create must
	// refuse it instead of overwriting the concurrent writer's value.
	store := &fakeAbsentStore{values: map[string]string{"late": "concurrent"}}

	err := createImports(context.Background(), store, map[string][]byte{
		"early": []byte("new"),
		"late":  []byte("imported"),
	})
	if err == nil || !strings.Contains(err.Error(), `"late"`) {
		t.Fatalf("expected a refusal naming the concurrent name, got %v", err)
	}
	if store.values["late"] != "concurrent" {
		t.Fatalf("concurrent value was overwritten: %q", store.values["late"])
	}
	if store.values["early"] != "new" {
		t.Fatalf("names before the refusal must still be created in sorted order, got %q", store.values["early"])
	}
}

func TestCreateImportsWritesNewNames(t *testing.T) {
	store := &fakeAbsentStore{values: map[string]string{}}

	if err := createImports(context.Background(), store, map[string][]byte{
		"a": []byte("1"),
		"b": []byte("2"),
	}); err != nil {
		t.Fatalf("createImports: %v", err)
	}
	if store.values["a"] != "1" || store.values["b"] != "2" {
		t.Fatalf("unexpected store: %v", store.values)
	}
}
