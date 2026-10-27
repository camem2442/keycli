package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/clip"
	"github.com/camem2442/keycli/internal/gitsetup"
	"github.com/camem2442/keycli/internal/kcerrors"
)

// Report is doctor --json output.
type Report struct {
	Profile        string          `json:"profile,omitempty"`
	ConfigPath     string          `json:"config_path"`
	StorePath      string          `json:"store_path"`
	KCKey          string          `json:"kc_key"`
	KCAge          string          `json:"kc_age"`
	IdentityPath   string          `json:"identity_path,omitempty"`
	Backends       []BackendStatus `json:"backends"`
	PathCollisions []string        `json:"path_collisions,omitempty"`
	Keychain       string          `json:"keychain"`
	Clipboard      string          `json:"clipboard"`
	GitHelper      string          `json:"git_helper"`
	GitHelperValue string          `json:"git_helper_value,omitempty"`
	GitHosts       []GitHostStatus `json:"git_hosts,omitempty"`
}

// GitHostStatus reports the configured git credential helper per mapped host.
type GitHostStatus struct {
	Host   string `json:"host"`
	Helper string `json:"helper"` // kc | other | unset
	Value  string `json:"value,omitempty"`
}

type BackendStatus struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Collect builds a diagnostic report (no secret values).
func Collect(ctx context.Context, a *app.App) Report {
	rep := Report{
		Profile:    detectProfile(),
		ConfigPath: a.ConfigPath,
		StorePath:  a.StorePath(),
		KCKey:      kcKeyStatus(),
	}
	rep.KCAge, rep.IdentityPath = identityStatus(ctx, a)
	rep.Backends = listBackends(ctx, a)
	rep.Keychain = keychainStatus(ctx, a)
	rep.Clipboard = clip.Status()
	rep.PathCollisions = pathCollisions()
	rep.GitHosts = gitHostStatuses(a)
	for _, h := range rep.GitHosts {
		if h.Host == gitsetup.DefaultHost {
			rep.GitHelper = h.Helper
			rep.GitHelperValue = h.Value
			break
		}
	}
	return rep
}

func classifyHelper(value string) string {
	switch {
	case value == "":
		return "unset"
	case strings.Contains(value, "kc") && strings.Contains(value, "git-credential"):
		return "kc"
	default:
		return "other"
	}
}

// gitHostStatuses reports the credential helper state for every mapped host.
// Values are git config strings only; no secret is read.
func gitHostStatuses(a *app.App) []GitHostStatus {
	hosts := a.Config.GitHosts()
	hasDefault := false
	for _, h := range hosts {
		if h == gitsetup.DefaultHost {
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		hosts = append(hosts, gitsetup.DefaultHost)
	}
	out := make([]GitHostStatus, 0, len(hosts))
	for _, h := range hosts {
		v, _ := gitsetup.ReadCurrentHelper(h)
		out = append(out, GitHostStatus{Host: h, Helper: classifyHelper(v), Value: v})
	}
	return out
}

func listBackends(ctx context.Context, a *app.App) []BackendStatus {
	backends, err := backend.Build(ctx, a.Config, a.Deps)
	if err != nil {
		return []BackendStatus{{Name: "?", Kind: "error", Available: false, Reason: err.Error()}}
	}
	var out []BackendStatus
	for _, b := range backends {
		ok, reason, err := b.Available(ctx)
		st := BackendStatus{Name: b.InstanceName(), Kind: b.Kind(), Available: ok, Reason: reason}
		if err != nil {
			st.Available = false
			st.Reason = err.Error()
		}
		out = append(out, st)
	}
	return out
}

func kcKeyStatus() string {
	if os.Getenv("KC_KEY") != "" {
		return "set"
	}
	return "unset"
}

// identityStatus reports the effective age identity source (KC_KEY,
// receiver.key, or keychain kc.age) without ever exposing the identity value.
func identityStatus(ctx context.Context, a *app.App) (string, string) {
	res, err := file.ResolveIdentity(ctx, file.IdentityOptions{
		Home:     a.Home,
		Config:   a.Config,
		Security: a.Deps.SecurityRunner,
	})
	if err != nil {
		if errors.Is(err, file.ErrNoIdentity) {
			return "missing", ""
		}
		switch kcerrors.Code(err) {
		case kcerrors.ExitLocked:
			return "locked", ""
		case kcerrors.ExitBlocked:
			return "blocked", ""
		default:
			return "missing", ""
		}
	}
	switch res.Source {
	case file.IdentitySourceEnv:
		return "env", ""
	case file.IdentitySourceFile:
		return "file", res.Path
	case file.IdentitySourceKeychain:
		return "keychain", ""
	default:
		return "missing", ""
	}
}

func keychainStatus(ctx context.Context, a *app.App) string {
	backends, _ := backend.Build(ctx, a.Config, a.Deps)
	for _, b := range backends {
		if b.Kind() != "keychain" {
			continue
		}
		ok, _, err := b.Available(ctx)
		if err != nil && kcerrors.Code(err) == kcerrors.ExitBlocked {
			return "blocked"
		}
		if ok {
			return "ok"
		}
		return "unavailable"
	}
	if runtime.GOOS != "darwin" {
		return "unavailable"
	}
	return "unavailable"
}

func pathCollisions() []string {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return nil
	}
	var hits []string
	seen := map[string]struct{}{}
	for _, dir := range filepath.SplitList(pathEnv) {
		for _, name := range []string{"kc"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err != nil {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			hits = append(hits, p)
		}
	}
	if len(hits) <= 1 {
		return nil
	}
	return hits
}

// PrintHuman writes a text report to w.
func PrintHuman(w *os.File, rep Report) {
	fmt.Fprintf(w, "config: %s\n", rep.ConfigPath)
	fmt.Fprintf(w, "store:  %s\n", rep.StorePath)
	fmt.Fprintf(w, "KC_KEY: %s\n", rep.KCKey)
	fmt.Fprintf(w, "identity: %s", rep.KCAge)
	if rep.IdentityPath != "" {
		fmt.Fprintf(w, " (%s)", rep.IdentityPath)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "keychain: %s\n", rep.Keychain)
	fmt.Fprintf(w, "clipboard: %s\n", rep.Clipboard)
	fmt.Fprintf(w, "git helper: %s", rep.GitHelper)
	if rep.GitHelperValue != "" {
		fmt.Fprintf(w, " (%s)", rep.GitHelperValue)
	}
	fmt.Fprintln(w)
	if len(rep.GitHosts) > 0 {
		fmt.Fprintln(w, "git hosts:")
		for _, h := range rep.GitHosts {
			fmt.Fprintf(w, "  - %s: %s", h.Host, h.Helper)
			if h.Value != "" {
				fmt.Fprintf(w, " (%s)", h.Value)
			}
			fmt.Fprintln(w)
		}
	}
	if len(rep.PathCollisions) > 0 {
		fmt.Fprintln(w, "PATH collisions:")
		for _, p := range rep.PathCollisions {
			fmt.Fprintf(w, "  - %s\n", p)
		}
	}
	fmt.Fprintln(w, "backends:")
	for _, b := range rep.Backends {
		if b.Available {
			fmt.Fprintf(w, "  - %s (%s): available\n", b.Name, b.Kind)
			continue
		}
		fmt.Fprintf(w, "  - %s (%s): unavailable", b.Name, b.Kind)
		if b.Reason != "" {
			fmt.Fprintf(w, " — %s", b.Reason)
		}
		fmt.Fprintln(w)
	}
}

// detectProfile classifies the runtime for consumer diagnostics (no secrets).
func detectProfile() string {
	switch runtime.GOOS {
	case "linux":
		if os.Getenv("GITHUB_ACTIONS") == "true" {
			return "linux-ci"
		}
		if os.Getenv("CURSOR_AGENT") != "" || os.Getenv("COMPOSER_REPO_URL") != "" {
			return "linux-cloud"
		}
		return "linux-headless"
	case "darwin":
		if os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" {
			return "mac-ssh"
		}
		return "mac-gui"
	default:
		return "unknown"
	}
}

// MarshalJSON encodes the report.
func MarshalJSON(rep Report) ([]byte, error) {
	return json.MarshalIndent(rep, "", "  ")
}
