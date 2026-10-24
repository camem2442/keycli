package keychain

import (
	"github.com/camem2442/keycli/internal/config"
	"github.com/camem2442/keycli/internal/testhooks"
)

// SecurityCommand returns the security CLI path (/usr/bin/security or KC_SECURITY when test hooks allow).
func SecurityCommand(cfg config.Config) string {
	if p := testhooks.Env(cfg, "KC_SECURITY"); p != "" {
		return p
	}
	return securityBin
}
