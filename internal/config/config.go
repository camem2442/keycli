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
	Plugin        string `toml:"plugin"`  // external: short name → kc-backend-<plugin>
	Command       string `toml:"command"` // external: explicit path or binary name (plugins dir, then PATH)
	Timeout       string `toml:"timeout"` // external: e.g. "30s"
	Enabled       *bool  `toml:"enabled"` // external: default true when listed in config
}

// Config is ~/.config/kc/config.toml.
type Config struct {
	Order          []string               `toml:"order"`
	Backend        map[string]BackendSpec `toml:"backend"`
	Alias          map[string][]string    `toml:"alias"`
	Link           map[string]LinkTarget  `toml:"link"`
	AllowTestHooks bool                   `toml:"allow_test_hooks"`
}

// Default returns sensible defaults when no file exists.
func Default(home string) Config {
	storePath := filepath.Join(home, ".kc", "store.age")
	return Config{
		Order: []string{"link", "env", "store"},
		Backend: map[string]BackendSpec{
			"link":  {Type: "link"},
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
	if c.Link == nil {
		c.Link = map[string]LinkTarget{}
	}
	EnsureLinkBackend(&c)
	for name, spec := range c.Backend {
		if spec.Path != "" {
			spec.Path = expandHome(spec.Path, home)
			c.Backend[name] = spec
		}
	}
	return c, path, nil
}

// Save writes config to path (0600).
func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var buf strings.Builder
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(buf.String()), 0o600)
}

// EnsureLinkBackend adds the link backend to order when links exist.
func EnsureLinkBackend(c *Config) {
	if len(c.Link) == 0 {
		return
	}
	if c.Backend == nil {
		c.Backend = map[string]BackendSpec{}
	}
	if _, ok := c.Backend["link"]; !ok {
		c.Backend["link"] = BackendSpec{Type: "link"}
	}
	hasLink := false
	for _, name := range c.Order {
		if name == "link" {
			hasLink = true
			break
		}
	}
	if !hasLink {
		c.Order = append([]string{"link"}, c.Order...)
	}
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
