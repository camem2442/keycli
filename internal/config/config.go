package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// BackendSpec configures one named backend instance.
type BackendSpec struct {
	Type          string `toml:"type"`
	Path          string `toml:"path"`
	ServicePrefix string `toml:"service_prefix"`
	Command       string `toml:"command"` // external kc-backend-* (reserved)
}

// Config is ~/.config/kc/config.toml.
type Config struct {
	Order   []string               `toml:"order"`
	Backend map[string]BackendSpec `toml:"backend"`
	Alias   map[string][]string    `toml:"alias"`
}

// Default returns sensible defaults when no file exists.
func Default(home string) Config {
	storePath := filepath.Join(home, ".kc", "store.age")
	return Config{
		Order: []string{"env", "store"},
		Backend: map[string]BackendSpec{
			"env":   {Type: "env"},
			"store": {Type: "file", Path: storePath},
		},
		Alias: map[string][]string{
			"github": {"gh", "GITHUB_TOKEN", "GH_TOKEN"},
		},
	}
}

// ConfigPath resolves the config file path (KC_CONFIG or ~/.config/kc/config.toml).
func ConfigPath(home string) string {
	if p := os.Getenv("KC_CONFIG"); p != "" {
		return expandHome(p, home)
	}
	return filepath.Join(home, ".config", "kc", "config.toml")
}

// Load reads config from path or returns defaults.
func Load(home string) (Config, string, error) {
	path := ConfigPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(home), path, nil
		}
		return Config{}, path, err
	}
	var c Config
	if _, err := toml.Decode(string(data), &c); err != nil {
		return Config{}, path, err
	}
	if len(c.Order) == 0 {
		def := Default(home)
		c.Order = def.Order
	}
	if c.Backend == nil {
		c.Backend = Default(home).Backend
	}
	if c.Alias == nil {
		c.Alias = map[string][]string{}
	}
	for name, spec := range c.Backend {
		if spec.Path != "" {
			spec.Path = expandHome(spec.Path, home)
			c.Backend[name] = spec
		}
	}
	return c, path, nil
}

func expandHome(p, home string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, strings.TrimPrefix(p, "~/"))
	}
	if p == "~" {
		return home
	}
	return p
}
