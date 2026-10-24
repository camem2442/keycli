package keychain

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/camem2442/keycli/internal/kcerrors"
)

// FindGenericPassword reads one keychain item via security(1).
func FindGenericPassword(ctx context.Context, runner backendSecurityRunner, service, account string) ([]byte, string, error) {
	var args []string
	if account != "" {
		args = []string{"find-generic-password", "-s", service, "-a", account, "-w"}
	} else {
		args = []string{"find-generic-password", "-s", service, "-w"}
	}
	out, err := runner.Run(ctx, args...)
	if err != nil {
		return nil, "", MapSecurityExit(err)
	}
	src := FormatSource("keychain", service, account)
	return []byte(strings.TrimSpace(string(out))), src, nil
}

// FormatSource builds keychain:service/account (account omitted when empty).
func FormatSource(prefix, service, account string) string {
	if account != "" {
		return prefix + ":" + service + "/" + account
	}
	return prefix + ":" + service
}

// AICoreLookupWithSource reads ai-core entries and reports where the value came from.
func AICoreLookupWithSource(ctx context.Context, runner backendSecurityRunner, n AICoreName) ([]byte, string, error) {
	if v, src, ok := aicoreEnvSource(n.Provider); ok {
		return v, src, nil
	}
	if os.Getenv("AI_CORE_NO_KEYCHAIN") == "1" {
		return nil, "", fmt.Errorf("%w", kcerrors.ErrNotFound)
	}
	var pairs []svcAcct
	for _, acct := range accountsForSlot(n.Provider, n.Slot) {
		pairs = append(pairs, svcAcct{servicePrimary(n.Provider), acct})
	}
	if n.Provider == "gemini" || n.Provider == "groq" {
		for _, acct := range legacyAccounts(n.Provider) {
			pairs = append(pairs, svcAcct{legacyService(n.Provider), acct})
		}
	}
	for _, p := range pairs {
		val, src, err := FindGenericPassword(ctx, runner, p.service, p.account)
		if err == nil {
			return val, src, nil
		}
		if errorsIsNotFound(err) {
			continue
		}
		return nil, "", err
	}
	return nil, "", fmt.Errorf("%w", kcerrors.ErrNotFound)
}

func aicoreEnvSource(provider string) ([]byte, string, bool) {
	keys := strings.ToUpper(provider) + "_API_KEYS"
	if v := os.Getenv(keys); v != "" {
		parts := strings.Split(v, ",")
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			return []byte(strings.TrimSpace(parts[0])), "env:" + keys, true
		}
	}
	single := strings.ToUpper(provider) + "_API_KEY"
	if v := os.Getenv(single); v != "" {
		return []byte(v), "env:" + single, true
	}
	return nil, "", false
}
