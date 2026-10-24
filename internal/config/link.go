package config

import (
	"fmt"
	"strings"
)

// LinkTarget maps a kc name to an external secret location (value never stored).
type LinkTarget struct {
	Type    string `toml:"type"` // "keychain"
	Service string `toml:"service"`
	Account string `toml:"account"`
}

// ParseLinkURI parses keychain:SERVICE[/ACCOUNT].
func ParseLinkURI(uri string) (LinkTarget, error) {
	const prefix = "keychain:"
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(uri)), prefix) {
		return LinkTarget{}, fmt.Errorf("link URI must start with keychain:")
	}
	rest := strings.TrimSpace(uri[len(prefix):])
	if rest == "" {
		return LinkTarget{}, fmt.Errorf("missing keychain service")
	}
	svc, acct, ok := strings.Cut(rest, "/")
	svc = strings.TrimSpace(svc)
	if svc == "" {
		return LinkTarget{}, fmt.Errorf("empty keychain service")
	}
	if !ok {
		return LinkTarget{Type: "keychain", Service: svc}, nil
	}
	return LinkTarget{Type: "keychain", Service: svc, Account: strings.TrimSpace(acct)}, nil
}
