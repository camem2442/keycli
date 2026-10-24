package middleware

import (
	"strings"

	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/middleware/audit"
)

// BuildChain returns middleware wrappers in config order, or nil when unset.
func BuildChain(home string, cfg config.Config, warn func(string)) []Middleware {
	if len(cfg.Middleware) == 0 {
		return nil
	}
	agent := DetectAgent()
	var out []Middleware
	for _, name := range cfg.Middleware {
		switch strings.ToLower(strings.TrimSpace(name)) {
		case "audit":
			w := audit.NewWriter(home, cfg, warn)
			out = append(out, auditMiddleware(w, agent))
		case "policy":
			p := NewPolicy(cfg, agent)
			out = append(out, p.Middleware())
		}
	}
	return out
}
