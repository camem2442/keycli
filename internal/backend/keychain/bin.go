package keychain

import "os"

// SecurityCommand returns the security CLI path (/usr/bin/security or KC_SECURITY).
func SecurityCommand() string {
	if p := os.Getenv("KC_SECURITY"); p != "" {
		return p
	}
	return securityBin
}
