package resolve_test

import (
	"testing"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/resolve"
)

func TestStrictCanonicalNameRejectsAmbiguousConfiguration(t *testing.T) {
	for name, aliases := range map[string]map[string][]string{
		"duplicate alias":       {"alpha": {"shared"}, "beta": {"shared"}},
		"case folded alias":     {"alpha": {"SHARED"}, "beta": {"shared"}},
		"canonical shadow":      {"alpha": {"beta"}, "beta": {"other"}},
		"case folded canonical": {"alpha": {}, "ALPHA": {}},
		"ambiguous fallback":    {"alpha": {"unique", "shared"}, "beta": {"shared"}},
		"empty alias":           {"alpha": {""}},
		"whitespace alias":      {"alpha": {" shared"}},
		"whitespace canonical":  {" alpha": {"shared"}},
		"control alias":         {"alpha": {"sh\x00ared"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := resolve.New(config.Config{Alias: aliases}, nil, t.TempDir(), nil)
			// Even a unique entry/canonical spelling must not enter a resolver whose
			// later fallback or policy canonicalization has an ambiguous namespace.
			for _, input := range []string{"shared", "alpha", "beta", "unique", "unrelated"} {
				if _, err := r.StrictCanonicalName(input); err == nil {
					t.Fatalf("ambiguous/invalid config accepted for %q", input)
				}
			}
		})
	}
}

func TestStrictCanonicalNameIsStableAndPreservesLegacy(t *testing.T) {
	cfg := config.Config{Alias: map[string][]string{"alpha": {"shared", "SHARED", "alpha"}, "beta": {"other"}}}
	r := resolve.New(cfg, nil, t.TempDir(), nil)
	for input, want := range map[string]string{"shared": "alpha", "SHARED": "alpha", " shared ": "alpha", "alpha": "alpha", "ALPHA": "alpha", "other": "beta", "PlainName": "PlainName"} {
		for i := 0; i < 128; i++ {
			got, err := r.StrictCanonicalName(input)
			if err != nil || got != want {
				t.Fatalf("input %q: got %q err %v", input, got, err)
			}
			if r.CanonicalName(got) != want {
				t.Fatal("validated canonical changed under legacy lookup")
			}
		}
	}
	if _, err := r.StrictCanonicalName("  "); err == nil {
		t.Fatal("empty input accepted")
	}
	// A legacy caller retains its old behavior; the new guard is explicit.
	legacy := resolve.New(config.Config{Alias: map[string][]string{"alpha": {"shared"}, "beta": {"shared"}}}, nil, t.TempDir(), nil)
	if got := legacy.CanonicalName("shared"); got != "alpha" && got != "beta" {
		t.Fatal("legacy resolver unexpectedly changed")
	}
}
