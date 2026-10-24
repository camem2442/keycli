package testhooks

import "github.com/camem2442/keycli/internal/config"

// Allowed reports whether test-only env overrides may be honored.
func Allowed(cfg config.Config) bool {
	return cfg.AllowTestHooks || enabledByBuildTag()
}

// Env returns envKey's value when test hooks are allowed, else "".
func Env(cfg config.Config, envKey string) string {
	if !Allowed(cfg) {
		return ""
	}
	return lookupEnv(envKey)
}
