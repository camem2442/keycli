package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/camem2442/keycli/internal/applecsv"
	"github.com/camem2442/keycli/internal/backend"
)

func TestPlanCSVImportSkipsRowsWithoutPassword(t *testing.T) {
	rows := []applecsv.Row{
		{Title: "GitHub", Username: "octocat", Password: "synthetic-only"},
		{Title: "No password", Username: "someone"},
	}
	plans, stats := planCSVImport(rows, canonNone, map[string]bool{}, false, false)
	if len(plans) != 1 || plans[0].name != "github" {
		t.Fatalf("plans = %v", plans)
	}
	if stats.skipped != 1 || stats.noPassword != 1 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestPlanCSVImportSuffixesDuplicateTitles(t *testing.T) {
	rows := []applecsv.Row{
		{Title: "Bank", Password: "one"},
		{Title: "bank", Password: "two"},
		{Title: "Bank!", Password: "three"},
	}
	plans, _ := planCSVImport(rows, canonNone, map[string]bool{}, false, false)
	var names []string
	for _, p := range plans {
		names = append(names, p.name)
	}
	if want := []string{"bank", "bank-2", "bank-3"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
}

func TestPlanCSVImportKeepsVerificationCodeOnlyOnRequest(t *testing.T) {
	rows := []applecsv.Row{{
		Title:    "Site",
		Password: "synthetic-only",
		OTPAuth:  "otpauth://totp?secret=SYNTHETIC&algorithm=SHA1&digits=6&period=30",
	}}
	plans, stats := planCSVImport(rows, canonNone, map[string]bool{}, false, false)
	if len(plans) != 1 || stats.otp != 1 {
		t.Fatalf("default must keep the code out of the store: %v %+v", plans, stats)
	}
	for _, p := range plans {
		if p.meta.Note != "" {
			t.Fatalf("code leaked into note: %q", p.meta.Note)
		}
	}

	plans, stats = planCSVImport(rows, canonNone, map[string]bool{}, true, false)
	if len(plans) != 2 || stats.otp != 0 {
		t.Fatalf("--keep-otp plans = %v stats = %+v", plans, stats)
	}
	if plans[1].name != "site.otp" || string(plans[1].value) != rows[0].OTPAuth || plans[1].meta.Kind != "otp" {
		t.Fatalf("otp plan = %+v", plans[1])
	}
	if plans[0].meta.Kind != "login" {
		t.Fatalf("login plan kind = %q", plans[0].meta.Kind)
	}
}

func TestCSVNoteKeepsNotesCellOnlyOnRequest(t *testing.T) {
	row := applecsv.Row{
		Username: "octocat",
		URL:      "https://example.test/",
		Notes:    "RECOVERY-CODE-AAAA-BBBB",
	}
	if got := csvNote(row, false); got != "" {
		t.Fatalf("default note = %q, want empty", got)
	}
	if got, want := csvNote(row, true), "RECOVERY-CODE-AAAA-BBBB"; got != want {
		t.Fatalf("--keep-notes note = %q, want %q", got, want)
	}
	if got := csvNote(applecsv.Row{}, false); got != "" {
		t.Fatalf("empty note = %q", got)
	}
}

func TestPlanCSVImportCanonicalisesAliasSoForceIsRequired(t *testing.T) {
	alias := func(name string) string {
		if name == "gh" {
			return "github"
		}
		return name
	}
	rows := []applecsv.Row{{Title: "gh", Password: "synthetic-only"}}
	plans, _ := planCSVImport(rows, alias, map[string]bool{"github": true}, false, false)
	if len(plans) != 1 || plans[0].name != "github" {
		t.Fatalf("alias not canonicalised: %+v", plans)
	}
	if got := conflictNames(plans, map[string]bool{"github": true}); len(got) != 1 {
		t.Fatalf("alias collision missed: %v", got)
	}
}

func TestPlanCSVImportSuffixAvoidsExistingName(t *testing.T) {
	rows := []applecsv.Row{
		{Title: "bank", Password: "one"},
		{Title: "bank", Password: "two"},
	}
	existing := map[string]bool{"bank-2": true}
	plans, _ := planCSVImport(rows, canonNone, existing, false, false)
	var names []string
	for _, p := range plans {
		names = append(names, p.name)
	}
	if want := []string{"bank", "bank-3"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	conflicts := conflictNames(plans, existing)
	if len(conflicts) != 0 {
		t.Fatalf("generated suffix counted as a conflict: %v", conflicts)
	}
}

func canonNone(name string) string { return name }

func TestConflictNamesSorted(t *testing.T) {
	plans := []csvPlan{{name: "b"}, {name: "a"}, {name: "c"}}
	existing := map[string]bool{"a": true, "c": true}
	if got := conflictNames(plans, existing); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatalf("conflicts = %v", got)
	}
}

func TestSummariseCapsList(t *testing.T) {
	if got := summarise([]string{"a", "b", "c"}, 2); got != "a, b, … (+1)" {
		t.Fatalf("summarise = %q", got)
	}
	if got := summarise([]string{"a"}, 2); got != "a" {
		t.Fatalf("summarise short = %q", got)
	}
}

func TestPlanCSVImportKeepsWhitespacePassword(t *testing.T) {
	plans, stats := planCSVImport([]applecsv.Row{{Title: "Fixture", Password: " 	 "}}, canonNone, nil, false, false)
	if len(plans) != 1 || string(plans[0].value) != " 	 " || stats.noPassword != 0 {
		t.Fatal("nonempty whitespace-only value must be preserved")
	}
}

type fakeCSVStore struct {
	present  map[string]bool
	replaced []string
}

func (f *fakeCSVStore) Set(_ context.Context, name string, _ []byte, _ backend.Meta) error {
	f.replaced = append(f.replaced, name)
	f.present[name] = true
	return nil
}

func (f *fakeCSVStore) SetIfAbsent(_ context.Context, name string, _ []byte, _ backend.Meta) error {
	if f.present[name] {
		return errors.New("concurrently added")
	}
	f.present[name] = true
	return nil
}

func TestCSVWriteNeverOverwritesUnlistedName(t *testing.T) {
	for _, force := range []bool{false, true} {
		store := &fakeCSVStore{present: map[string]bool{"new": true}}
		if err := writeCSVPlans(context.Background(), store, []csvPlan{{name: "new"}}, nil, force); err == nil || len(store.replaced) != 0 {
			t.Fatal("a name created after listing must not be overwritten")
		}
	}
	store := &fakeCSVStore{present: map[string]bool{"known": true}}
	if err := writeCSVPlans(context.Background(), store, []csvPlan{{name: "known"}}, map[string]bool{"known": true}, true); err != nil ||
		!reflect.DeepEqual(store.replaced, []string{"known"}) {
		t.Fatal("an existing conflict still supports explicit force")
	}
}
