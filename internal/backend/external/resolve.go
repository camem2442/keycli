package external

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const binaryPrefix = "kc-backend-"

// PluginNameFromBinary strips the kc-backend- prefix.
func PluginNameFromBinary(base string) string {
	base = strings.TrimPrefix(base, binaryPrefix)
	return base
}

// BinaryName returns the executable name for a plugin short name.
func BinaryName(plugin string) string {
	plugin = strings.TrimSpace(plugin)
	if strings.HasPrefix(plugin, binaryPrefix) {
		return plugin
	}
	return binaryPrefix + plugin
}

// ResolveCommand finds an enabled plugin binary. PATH is only consulted when config
// explicitly references the plugin (never for auto-run).
func ResolveCommand(home, plugin, command string) (string, error) {
	if cmd := strings.TrimSpace(command); cmd != "" {
		if strings.Contains(cmd, string(os.PathSeparator)) || filepath.IsAbs(cmd) {
			if _, err := os.Stat(cmd); err != nil {
				return "", fmt.Errorf("plugin command not found: %s", cmd)
			}
			return cmd, nil
		}
		// Bare name: plugins dir first, then PATH.
		if p, err := resolveInPluginsDir(home, cmd); err == nil {
			return p, nil
		}
		if p, err := lookPath(cmd); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("plugin command not found: %s", cmd)
	}
	plug := strings.TrimSpace(plugin)
	if plug == "" {
		return "", fmt.Errorf("external backend requires plugin or command")
	}
	bin := BinaryName(plug)
	if p, err := resolveInPluginsDir(home, bin); err == nil {
		return p, nil
	}
	if p, err := lookPath(bin); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("plugin %q not found in ~/.kc/plugins or PATH", plug)
}

func resolveInPluginsDir(home, name string) (string, error) {
	p := filepath.Join(home, ".kc", "plugins", name)
	if _, err := os.Stat(p); err != nil {
		return "", err
	}
	return p, nil
}

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}

// Discover scans PATH and ~/.kc/plugins for kc-backend-* executables.
func Discover(home string) ([]Discovered, error) {
	seen := map[string]Discovered{}
	add := func(path string) {
		base := filepath.Base(path)
		if !strings.HasPrefix(base, binaryPrefix) {
			return
		}
		name := PluginNameFromBinary(base)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = Discovered{Name: name, Path: path}
	}
	pluginsDir := filepath.Join(home, ".kc", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			add(filepath.Join(pluginsDir, e.Name()))
		}
	}
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(dir, binaryPrefix+"*"))
		for _, m := range matches {
			add(m)
		}
	}
	out := make([]Discovered, 0, len(seen))
	for _, d := range seen {
		out = append(out, d)
	}
	return out, nil
}

// Discovered is a plugin binary found on disk (not necessarily enabled).
type Discovered struct {
	Name string
	Path string
}
