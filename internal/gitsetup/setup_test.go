package gitsetup

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlan(t *testing.T) {
	p := BuildPlan("/usr/local/bin/kc", "")
	if p.Host != DefaultHost {
		t.Fatalf("host %q", p.Host)
	}
	if p.Key != "credential.https://github.com.helper" {
		t.Fatalf("key %q", p.Key)
	}
	if p.HelperValue != "!/usr/local/bin/kc git-credential" {
		t.Fatalf("%q", p.HelperValue)
	}
	if p.GhSnippet == "" {
		t.Fatal("empty gh snippet")
	}
}

func TestBuildPlanHost(t *testing.T) {
	p := BuildPlan("/usr/local/bin/kc", "HTTPS://Origin.Cursor.com/git")
	if p.Host != "origin.cursor.com" {
		t.Fatalf("host %q", p.Host)
	}
	if p.Key != "credential.https://origin.cursor.com.helper" {
		t.Fatalf("key %q", p.Key)
	}
	if p.HelperValue != "!/usr/local/bin/kc git-credential" {
		t.Fatalf("%q", p.HelperValue)
	}
}

func TestHelperKeyNormalizes(t *testing.T) {
	if got := HelperKey(" GitHub.com "); got != "credential.https://github.com.helper" {
		t.Fatalf("%q", got)
	}
}

// isolateGit points --global config at a temp file so tests never touch the
// real user gitconfig.
func isolateGit(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
}

func TestApplyUndo(t *testing.T) {
	isolateGit(t)
	plan := BuildPlan("/usr/local/bin/kc", "origin.test")
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCurrentHelper("origin.test")
	if err != nil || got != "!/usr/local/bin/kc git-credential" {
		t.Fatalf("%q %v", got, err)
	}
	out, err := exec.Command("git", "config", "--global", "--get-all", plan.Key).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "" {
		t.Fatalf("want reset entry then helper, got %q", lines)
	}
	if err := Undo(plan.KcPath, "origin.test"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadCurrentHelper("origin.test"); got != "" {
		t.Fatalf("helper still set: %q", got)
	}
}

func TestUndoRefusesForeignHelper(t *testing.T) {
	isolateGit(t)
	key := HelperKey("origin.test")
	if err := exec.Command("git", "config", "--global", key, "!/usr/bin/other-helper").Run(); err != nil {
		t.Fatal(err)
	}
	if err := Undo("/usr/local/bin/kc", "origin.test"); err == nil {
		t.Fatal("expected refusal for helper not managed by kc")
	}
}
