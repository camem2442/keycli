package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/backend/keychain"
	"github.com/camem2442/keycli/internal/clip"
	"github.com/camem2442/keycli/internal/gitsetup"
	"github.com/camem2442/keycli/internal/kcerrors"
)

// Report is doctor --json output.
type Report struct {
	ConfigPath     string          `json:"config_path"`
	StorePath      string          `json:"store_path"`
	KCKey          string          `json:"kc_key"`
	KCAge          string          `json:"kc_age"`
	Backends       []BackendStatus `json:"backends"`
	PathCollisions []string        `json:"path_collisions,omitempty"`
	Keychain       string          `json:"keychain"`
	Clipboard      string          `json:"clipboard"`
	GitHelper      string          `json:"git_helper"`
	GitHelperValue string          `json:"git_helper_value,omitempty"`
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
		ConfigPath: a.ConfigPath,
		StorePath:  a.StorePath(),
		KCKey:      kcKeyStatus(),
		KCAge:      kcAgeStatus(ctx, a),
		Backends:   listBackends(ctx, a),
		Keychain:   keychainStatus(ctx, a),
		Clipboard:  clip.Status(),
	}
	rep.PathCollisions = pathCollisions()
	helper, _ := gitsetup.ReadCurrentHelper()
	rep.GitHelperValue = helper
	switch {
	case helper == "":
		rep.GitHelper = "unset"
	case strings.Contains(helper, "kc") && strings.Contains(helper, "git-credential"):
		rep.GitHelper = "kc"
	default:
		rep.GitHelper = "other"
	}
	return rep
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

func kcAgeStatus(ctx context.Context, a *app.App) string {
	if os.Getenv("KC_KEY") != "" {
		return "env"
	}
	if a.Deps.SecurityRunner == nil {
		return "missing"
	}
	_, _, err := keychain.FindGenericPassword(ctx, a.Deps.SecurityRunner, "kc.age", "kc")
	if err == nil {
		return "keychain"
	}
	if kcerrors.Code(err) == kcerrors.ExitBlocked {
		return "blocked"
	}
	if runtime.GOOS != "darwin" {
		return "missing"
	}
	return "missing"
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
	fmt.Fprintf(w, "kc.age: %s\n", rep.KCAge)
	fmt.Fprintf(w, "keychain: %s\n", rep.Keychain)
	fmt.Fprintf(w, "clipboard: %s\n", rep.Clipboard)
	fmt.Fprintf(w, "git helper: %s", rep.GitHelper)
	if rep.GitHelperValue != "" {
		fmt.Fprintf(w, " (%s)", rep.GitHelperValue)
	}
	fmt.Fprintln(w)
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

// MarshalJSON encodes the report.
func MarshalJSON(rep Report) ([]byte, error) {
	return json.MarshalIndent(rep, "", "  ")
}
