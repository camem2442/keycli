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

func TestPolicyEnabledRequiresMiddleware(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"codex": {Allow: []string{"github"}},
	}
	if middleware.PolicyEnabled(cfg) {
		t.Fatal("rules without middleware must not be enforced")
	}
	cfg.Middleware = []string{"audit", " POLICY "}
	if !middleware.PolicyEnabled(cfg) {
		t.Fatal("policy middleware should enable enforcement")
	}
	cfg.Middleware = []string{"audit"}
	cfg.Policy.Agent = nil
	if middleware.PolicyEnabled(cfg) {
		t.Fatal("middleware without rules must not enforce")
	}
}

func TestPolicyConfiguredDefaultAppliesToEveryUnregisteredAgent(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Middleware = []string{"audit", "policy"}
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"codex":   {Allow: []string{"github_agent"}},
		"default": {Allow: []string{"public_fixture"}},
	}
	for _, agent := range []string{"antigravity", "opencode", "cursor", "claude", "ci", "human", "unknown", "custom-cloud-agent"} {
		p := middleware.NewPolicy(cfg, agent)
		if p.Allowed("cloudflare_billing_read") {
			t.Fatalf("unregistered %s bypassed default deny", agent)
		}
		if !p.Allowed("public_fixture") {
			t.Fatalf("unregistered %s did not inherit default allow", agent)
		}
	}
	codex := middleware.NewPolicy(cfg, "codex")
	if !codex.Allowed("github_agent") || codex.Allowed("public_fixture") {
		t.Fatal("registered codex allowlist must override default")
	}
}

func TestPolicyExplicitHumanOverridesDefaultDeny(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Middleware = []string{"audit", "policy"}
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"default": {Allow: []string{}},
		"human":   {Allow: []string{"*"}},
	}
	owner := middleware.NewPolicy(cfg, "human")
	if !owner.Allowed("github_agent") || !owner.Allowed("cloudflare_billing_read") {
		t.Fatal("explicit interactive owner profile must retain configured access")
	}
	for _, agent := range []string{"unknown", "cursor", "antigravity"} {
		if middleware.NewPolicy(cfg, agent).Allowed("github_agent") {
			t.Fatalf("%s unexpectedly bypasses configured default-deny rule", agent)
		}
	}
}

func TestPolicyDenyOverridesBroadAllow(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"cursor": {Allow: []string{"*"}, Deny: []string{"github", "sogang-*"}},
	}
	p := middleware.NewPolicy(cfg, "cursor")
	if p.Allowed("github") {
		t.Fatal("explicit deny must override allow=*")
	}
	if p.Allowed("sogang-pw") {
		t.Fatal("deny glob must match")
	}
	for _, name := range []string{"github_agent", "dev_cf_worker_read", "cloudflare_agent"} {
		if !p.Allowed(name) {
			t.Fatalf("%s should stay allowed under allow=*", name)
		}
	}
}

func TestPolicyDenyOverridesNarrowAllow(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"cursor": {Allow: []string{"dev_*"}, Deny: []string{"dev_github_oauth_client_secret"}},
	}
	p := middleware.NewPolicy(cfg, "cursor")
	if p.Allowed("dev_github_oauth_client_secret") {
		t.Fatal("deny must win even when allow matches")
	}
	if !p.Allowed("dev_cf_worker_read") {
		t.Fatal("non-denied allow match should pass")
	}
	if p.Allowed("github") {
		t.Fatal("names outside allow stay denied")
	}
}

func TestPolicyDenyOnlyIsStillDenyByDefault(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"cursor": {Deny: []string{"github"}},
	}
	p := middleware.NewPolicy(cfg, "cursor")
	if p.Allowed("anything") {
		t.Fatal("deny-only rule must not imply allow")
	}
}

func TestPolicyDenyIsCaseInsensitive(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"cursor": {Allow: []string{"*"}, Deny: []string{"GitHub"}},
	}
	if middleware.NewPolicy(cfg, "cursor").Allowed("github") {
		t.Fatal("deny should match case-insensitively")
	}
}

func TestPolicyEmptyDenyKeepsAllowBehaviour(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"cursor": {Allow: []string{"github_agent"}, Deny: []string{}},
	}
	p := middleware.NewPolicy(cfg, "cursor")
	if !p.Allowed("github_agent") || p.Allowed("github") {
		t.Fatal("empty deny list must not change allow semantics")
	}
}

// A malformed deny pattern cannot be evaluated, so it must fail closed instead
// of letting the broad allow beneath it apply.
func TestPolicyMalformedDenyFailsClosed(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"cursor": {Allow: []string{"*"}, Deny: []string{"sogang-[abc"}},
	}
	p := middleware.NewPolicy(cfg, "cursor")
	for _, name := range []string{"github", "github_agent", "dev_cf_worker_read"} {
		if p.Allowed(name) {
			t.Fatalf("%s must be denied when a deny pattern is malformed", name)
		}
	}
}

// A malformed allow pattern must not match anything; the agent stays denied.
func TestPolicyMalformedAllowDoesNotMatch(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Policy.Agent = map[string]config.PolicyAgentSettings{
		"cursor": {Allow: []string{"dev_[abc"}},
	}
	if middleware.NewPolicy(cfg, "cursor").Allowed("dev_cf_worker_read") {
		t.Fatal("malformed allow pattern must not match")
	}
}
