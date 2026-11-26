package cli

import (
	"reflect"
	"testing"

	"github.com/camem2442/keycli/internal/applecsv"
)

func TestPlanCSVImportSkipsRowsWithoutPassword(t *testing.T) {
	rows := []applecsv.Row{
		{Title: "GitHub", Username: "octocat", Password: "synthetic-only"},
		{Title: "No password", Username: "someone"},
	}
	plans, stats := planCSVImport(rows, map[string]bool{}, false)
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
	plans, _ := planCSVImport(rows, map[string]bool{}, false)
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
	plans, stats := planCSVImport(rows, map[string]bool{}, false)
	if len(plans) != 1 || stats.otp != 1 {
		t.Fatalf("default must keep the code out of the store: %v %+v", plans, stats)
	}
	for _, p := range plans {
		if p.note != "" {
			t.Fatalf("code leaked into note: %q", p.note)
		}
	}

	plans, stats = planCSVImport(rows, map[string]bool{}, true)
	if len(plans) != 2 || stats.otp != 0 {
		t.Fatalf("--keep-otp plans = %v stats = %+v", plans, stats)
	}
	if plans[1].name != "site.otp" || string(plans[1].value) != rows[0].OTPAuth {
		t.Fatalf("otp plan = %+v", plans[1])
	}
}

func TestCSVNoteCarriesMetadataNotSecrets(t *testing.T) {
	note := csvNote(applecsv.Row{
		Username: "octocat",
		URL:      "https://example.test/",
		Notes:    "personal",
	})
	if want := "username=octocat | url=https://example.test/ | personal"; note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}
	if got := csvNote(applecsv.Row{}); got != "" {
		t.Fatalf("empty note = %q", got)
	}
}

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
