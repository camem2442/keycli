package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/camem2442/keycli/internal/backend/external"
	"github.com/camem2442/keycli/internal/config"
)

// Entry is one row for `kc plugin list`.
type Entry struct {
	Name             string                 `json:"name"`
	Path             string                 `json:"path"`
	Enabled          bool                   `json:"enabled"`
	BackendInstances []string               `json:"backend_instances,omitempty"`
	Protocol         int                    `json:"protocol,omitempty"`
	Capabilities     *external.Capabilities `json:"capabilities,omitempty"`
	ProbeError       string                 `json:"probe_error,omitempty"`
}

// List discovers plugins and probes enabled ones.
func List(ctx context.Context, home string, cfg config.Config, runner external.Runner) ([]Entry, error) {
	discovered, err := external.Discover(home)
	if err != nil {
		return nil, err
	}
	enabled := external.EnabledBackends(cfg)
	byName := map[string]external.Discovered{}
	for _, d := range discovered {
		byName[d.Name] = d
	}
	// Config may reference plugins not on disk yet.
	for name, insts := range enabled {
		if _, ok := byName[name]; !ok {
			byName[name] = external.Discovered{Name: name, Path: ""}
		}
		_ = insts
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	if runner == nil {
		runner = external.ExecRunner{Timeout: 5 * time.Second}
	}
	var out []Entry
	for _, name := range names {
		d := byName[name]
		insts := enabled[name]
		ent := Entry{
			Name:             name,
			Path:             d.Path,
			Enabled:          len(insts) > 0,
			BackendInstances: insts,
		}
		if ent.Enabled && d.Path != "" {
			inst := ""
			if len(insts) > 0 {
				inst = insts[0]
			}
			caps, pv, err := external.Handshake(ctx, runner, d.Path, inst)
			if err != nil {
				ent.ProbeError = err.Error()
			} else {
				ent.Protocol = pv
				ent.Capabilities = &caps
			}
		}
		out = append(out, ent)
	}
	return out, nil
}

// PrintHuman writes a text table.
func PrintHuman(w *os.File, entries []Entry) {
	for _, e := range entries {
		state := "disabled"
		if e.Enabled {
			state = "enabled"
		}
		fmt.Fprintf(w, "%s (%s)", e.Name, state)
		if e.Path != "" {
			fmt.Fprintf(w, " — %s", e.Path)
		}
		fmt.Fprintln(w)
		if len(e.BackendInstances) > 0 {
			fmt.Fprintf(w, "  backends: %s\n", strings.Join(e.BackendInstances, ", "))
		}
		if e.Protocol > 0 {
			fmt.Fprintf(w, "  protocol: v%d\n", e.Protocol)
		}
		if e.Capabilities != nil {
			fmt.Fprintf(w, "  capabilities: write=%v list=%v network=%v\n",
				e.Capabilities.CanWrite, e.Capabilities.CanList, e.Capabilities.NeedsNetwork)
		}
		if e.ProbeError != "" {
			fmt.Fprintf(w, "  probe: %s\n", e.ProbeError)
		}
	}
}

// MarshalJSON encodes entries.
func MarshalJSON(entries []Entry) ([]byte, error) {
	return json.MarshalIndent(entries, "", "  ")
}
