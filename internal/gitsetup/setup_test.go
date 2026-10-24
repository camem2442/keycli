package gitsetup

import "testing"

func TestBuildPlan(t *testing.T) {
	p := BuildPlan("/usr/local/bin/kc")
	if p.HelperValue != "!/usr/local/bin/kc git-credential" {
		t.Fatalf("%q", p.HelperValue)
	}
	if p.GhSnippet == "" {
		t.Fatal("empty gh snippet")
	}
}
