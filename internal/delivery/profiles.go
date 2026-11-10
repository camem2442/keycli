// Package delivery builds advisory delivery plans from local metadata.
package delivery

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

const (
	githubWIF         = "github-actions-wif"
	cloudflareWorkers = "cloudflare-workers"
	ociWIF            = "oci-instance-wif"
)

type Profiles struct {
	entries map[string]profile
}

type profile struct {
	Provider            string `toml:"provider"`
	Repository          string `toml:"repository"`
	Ref                 string `toml:"ref"`
	WorkflowRef         string `toml:"workflow_ref"`
	Environment         string `toml:"environment"`
	Audience            string `toml:"audience"`
	WIFProviderAlias    string `toml:"wif_provider_alias"`
	ServiceAccountAlias string `toml:"service_account_alias"`
	AccountAlias        string `toml:"account_alias"`
	Worker              string `toml:"worker"`
	Source              string `toml:"source"`
	Destination         string `toml:"destination"`
	Effect              string `toml:"effect"`
	TenancyAlias        string `toml:"tenancy_alias"`
	InstanceAlias       string `toml:"instance_alias"`
	IdentityDomainAlias string `toml:"identity_domain_alias"`
}

var (
	labelPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	repoPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)
	workerPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	envNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)
	workflowPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.ya?ml$`)
)

// ParseProfiles accepts only version 1 and rejects fields outside the selected
// provider's contract, even when an unexpected field has an empty value.
func ParseProfiles(data []byte) (Profiles, error) {
	var doc struct {
		Version  int                `toml:"schema_version"`
		Profiles map[string]profile `toml:"profiles"`
	}
	meta, err := toml.Decode(string(data), &doc)
	if err != nil {
		// TOML parse errors can quote input values. Keep them out of CLI output.
		return Profiles{}, fmt.Errorf("invalid delivery TOML (syntax, type, or duplicate key)")
	}
	if len(meta.Undecoded()) > 0 {
		return Profiles{}, fmt.Errorf("unknown delivery configuration field")
	}
	if doc.Version != 1 || len(doc.Profiles) == 0 {
		return Profiles{}, fmt.Errorf("delivery configuration requires schema_version 1 and at least one profile")
	}

	// TOML's struct decoder matches fields case-insensitively. Validate the
	// exact key spelling and provider scope using the original key paths.
	fields := make(map[string][]string, len(doc.Profiles))
	for _, key := range meta.Keys() {
		switch {
		case len(key) == 1 && key[0] == "schema_version":
		case len(key) <= 2 && key[0] == "profiles":
		case len(key) == 3 && key[0] == "profiles":
			fields[key[1]] = append(fields[key[1]], key[2])
		default:
			return Profiles{}, fmt.Errorf("unknown delivery configuration field")
		}
	}

	names := make([]string, 0, len(doc.Profiles))
	for name := range doc.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		folded := strings.ToLower(name)
		if !labelPattern.MatchString(name) || seen[folded] {
			return Profiles{}, fmt.Errorf("invalid or ambiguous delivery profile name")
		}
		seen[folded] = true
		if err := doc.Profiles[name].validate(fields[name]); err != nil {
			return Profiles{}, fmt.Errorf("profile %q: %w", name, err)
		}
	}
	return Profiles{entries: doc.Profiles}, nil
}

func (p profile) validate(fields []string) error {
	var allowed string
	switch p.Provider {
	case githubWIF:
		allowed = "repository ref workflow_ref environment audience wif_provider_alias service_account_alias"
		if !validRepository(p.Repository) || !validRef(p.Ref) ||
			!validWorkflowRef(p.Repository, p.Ref, p.WorkflowRef) ||
			!labelPattern.MatchString(p.Environment) || !validAudience(p.Audience) ||
			!labelPattern.MatchString(p.WIFProviderAlias) || !labelPattern.MatchString(p.ServiceAccountAlias) {
			return fmt.Errorf("invalid GitHub repository, ref, workflow, environment, audience, or WIF alias")
		}
	case cloudflareWorkers:
		allowed = "account_alias worker environment source destination effect"
		if !labelPattern.MatchString(p.AccountAlias) || !workerPattern.MatchString(p.Worker) ||
			!labelPattern.MatchString(p.Environment) || !labelPattern.MatchString(p.Source) ||
			!envNamePattern.MatchString(p.Destination) || p.Effect != "inactive-version" {
			return fmt.Errorf("invalid Cloudflare account, Worker, environment, source, destination, or effect")
		}
	case ociWIF:
		allowed = "tenancy_alias instance_alias identity_domain_alias wif_provider_alias service_account_alias"
		for _, value := range []string{p.TenancyAlias, p.InstanceAlias, p.IdentityDomainAlias, p.WIFProviderAlias, p.ServiceAccountAlias} {
			if !labelPattern.MatchString(value) {
				return fmt.Errorf("invalid OCI tenancy, instance, identity-domain, or WIF alias")
			}
		}
	default:
		return fmt.Errorf("unsupported delivery provider")
	}
	for _, field := range fields {
		if field != "provider" && !strings.Contains(" "+allowed+" ", " "+field+" ") {
			return fmt.Errorf("delivery field is outside the provider scope")
		}
	}
	return nil
}

func validRepository(value string) bool {
	if !repoPattern.MatchString(value) {
		return false
	}
	_, name, _ := strings.Cut(value, "/")
	return name != "." && name != ".."
}

func validRef(ref string) bool {
	var name string
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		name = strings.TrimPrefix(ref, "refs/heads/")
	case strings.HasPrefix(ref, "refs/tags/"):
		name = strings.TrimPrefix(ref, "refs/tags/")
	default:
		return false
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "@{") ||
		strings.ContainsAny(ref, "~^:?*[\\") || strings.HasSuffix(ref, ".") ||
		strings.ContainsFunc(ref, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

func validWorkflowRef(repository, ref, workflow string) bool {
	path, ok := strings.CutPrefix(workflow, repository+"/.github/workflows/")
	if !ok {
		return false
	}
	filename, ok := strings.CutSuffix(path, "@"+ref)
	return ok && workflowPattern.MatchString(filename)
}

func validAudience(audience string) bool {
	return len(audience) > 0 && len(audience) <= 512 &&
		!strings.ContainsFunc(audience, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}
