package middleware_test

import (
	"testing"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/middleware"
)

func TestPolicyNoSectionAllowsAll(t *testing.T) {
	cfg := config.Default(t.TempDir())
	p := middleware.NewPolicy(cfg, "codex")
	if !p.Allowed("anything") {
		t.Fatal("expected allow without policy section")
	}
}

func TestPolicyGlobAllow(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"codex": {Allow: []string{"github", "groq*"}},
	}
	p := middleware.NewPolicy(cfg, "codex")
	if !p.Allowed("github") {
		t.Fatal("github")
	}
	if !p.Allowed("groq") {
		t.Fatal("groq")
	}
	if p.Allowed("openai") {
		t.Fatal("openai should deny")
	}
}

func TestPolicyUnknownUsesDefault(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"default": {Allow: []string{"demo"}},
	}
	p := middleware.NewPolicy(cfg, "unknown")
	if !p.Allowed("demo") {
		t.Fatal("default allow")
	}
	if p.Allowed("other") {
		t.Fatal("default deny")
	}
}

func TestPolicyAgentWithoutEntryAllowed(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"codex": {Allow: []string{"github"}},
	}
	p := middleware.NewPolicy(cfg, "cursor")
	if !p.Allowed("openai") {
		t.Fatal("cursor has no policy entry")
	}
}
