package config

import (
	"os"
	"testing"
)

func TestNormalizeGitHost(t *testing.T) {
	cases := map[string]string{
		"GitHub.com":                      "github.com",
		" https://origin.cursor.com/git ": "origin.cursor.com",
		"https://user@example.com:8443/x": "example.com:8443",
		"":                                "",
	}
	for in, want := range cases {
		if got := NormalizeGitHost(in); got != want {
			t.Fatalf("NormalizeGitHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitHostLookup(t *testing.T) {
	c := Config{Git: map[string]GitHostSpec{
		"origin.cursor.com": {Secret: "cursor_api_key", Username: "x-access-token", Command: "origin auth token"},
	}}
	spec, ok := c.GitHost("HTTPS://origin.cursor.com/git/x/y")
	if !ok {
		t.Fatal("expected match")
	}
	if spec.Secret != "cursor_api_key" || spec.Username != "x-access-token" || spec.Command != "origin auth token" {
		t.Fatalf("unexpected spec %+v", spec)
	}
	if spec, ok := c.GitHost("github.com"); !ok || !spec.Implicit || spec.Secret != "github" {
		t.Fatalf("implicit github.com fallback: %+v %v", spec, ok)
	}
	if _, ok := c.GitHost(""); ok {
		t.Fatal("empty host must not match")
	}
}

func TestGitHostsSorted(t *testing.T) {
	c := Config{Git: map[string]GitHostSpec{
		"b.example": {Secret: "b"},
		"A.example": {Secret: "a"},
	}}
	got := c.GitHosts()
	if len(got) != 2 || got[0] != "a.example" || got[1] != "b.example" {
		t.Fatalf("%v", got)
	}
}

func TestDefaultGitMapping(t *testing.T) {
	c := Default("/home/x")
	spec, ok := c.GitHost("github.com")
	if !ok || spec.Secret != "github" || spec.Username != "git" || !spec.Implicit {
		t.Fatalf("%+v %v", spec, ok)
	}
	if len(c.Git) != 0 {
		t.Fatalf("default must not write explicit git mappings: %+v", c.Git)
	}
}

func TestLoadKeepsDefaultGitHost(t *testing.T) {
	home := t.TempDir()
	path := home + "/config.toml"
	body := `
[git."origin.cursor.com"]
secret = "cursor_api_key"
username = "x-access-token"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KC_CONFIG", path)
	c, _, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := c.GitHost("github.com")
	if !ok || !spec.Implicit {
		t.Fatalf("github.com fallback must survive a custom host file: %+v %v", spec, ok)
	}
	if _, ok := c.Git["github.com"]; ok {
		t.Fatal("Load must not inject github.com into explicit config")
	}
	if spec, ok := c.GitHost("origin.cursor.com"); !ok || spec.Secret != "cursor_api_key" || spec.Implicit {
		t.Fatalf("origin mapping missing: %+v %v", spec, ok)
	}
}

func TestEnsureGitHostRecordsExplicitMapping(t *testing.T) {
	c := Default("/home/x")
	spec, added := c.EnsureGitHost("https://GitHub.com/x")
	if !added || spec.Secret != "github" || spec.Implicit {
		t.Fatalf("%+v added=%v", spec, added)
	}
	if got, ok := c.Git["github.com"]; !ok || got.Implicit || got.Secret != "github" {
		t.Fatalf("explicit mapping not recorded: %+v", c.Git)
	}
	if _, added := c.EnsureGitHost("github.com"); added {
		t.Fatal("existing explicit mapping must not be replaced")
	}

	spec, added = c.EnsureGitHost("origin.test")
	if !added || spec.Secret != "origin.test" || spec.Username != "git" || spec.Implicit {
		t.Fatalf("%+v added=%v", spec, added)
	}
}
