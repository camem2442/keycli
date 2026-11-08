package config

import (
	"reflect"
	"testing"
)

func TestAliasErrorsOwnership(t *testing.T) {
	cases := []struct {
		name    string
		aliases map[string][]string
		wantErr bool
	}{
		{"empty", nil, false},
		{"normal", map[string][]string{"github": {"gh", "GH_TOKEN"}, "demo": {"DEMO_TOKEN"}}, false},
		{"same-owner-repeat", map[string][]string{"github": {"gh", "GH", "github"}}, false},
		{"shared-alias", map[string][]string{"allowed": {"shared"}, "denied": {"shared"}}, true},
		{"case-collision", map[string][]string{"allowed": {"Shared"}, "denied": {"SHARED"}}, true},
		{"canonical-shadow", map[string][]string{"allowed": {"denied"}, "denied": {}}, true},
		{"canonical-case", map[string][]string{"GitHub": {}, "github": {}}, true},
		{"normalized-collision", map[string][]string{"allowed": {" shared "}, "denied": {"shared"}}, true},
		{"blank", map[string][]string{"allowed": {" "}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Alias: tc.aliases}
			got := cfg.AliasErrors()
			if (len(got) > 0) != tc.wantErr {
				t.Fatalf("AliasErrors() = %v, want error %t", got, tc.wantErr)
			}
		})
	}
}

func TestAliasErrorsDeterministic(t *testing.T) {
	a := Config{Alias: map[string][]string{"z": {"shared", "a"}, "a": {"SHARED"}}}
	b := Config{Alias: map[string][]string{"a": {"SHARED"}, "z": {"shared", "a"}}}
	want := a.AliasErrors()
	if len(want) != 2 {
		t.Fatalf("expected two ownership conflicts, got %v", want)
	}
	for i := 0; i < 100; i++ {
		if got := b.AliasErrors(); !reflect.DeepEqual(got, want) {
			t.Fatalf("alias diagnostics changed with map iteration: %v vs %v", got, want)
		}
	}
}
