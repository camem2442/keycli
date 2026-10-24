package keychain

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/camem2442/keycli/internal/kcerrors"
)

// Known AI-core providers (from ai-core keychain.py).
var knownProviders = map[string]struct{}{
	"gemini": {}, "groq": {}, "cerebras": {}, "openai": {}, "anthropic": {},
}

// AICoreName parses kc names like "openai" or "groq#3".
// Slot 1 may be written as "openai" or "openai#1".
type AICoreName struct {
	Provider string
	Slot     int // 1–10
}

// ParseAICoreName returns provider/slot if name refers to ai-core.
func ParseAICoreName(name string) (AICoreName, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return AICoreName{}, false
	}
	provider := name
	slot := 1
	if i := strings.Index(name, "#"); i >= 0 {
		provider = name[:i]
		n, err := strconv.Atoi(name[i+1:])
		if err != nil || n < 1 || n > 10 {
			return AICoreName{}, false
		}
		slot = n
	}
	if _, ok := knownProviders[provider]; !ok {
		return AICoreName{}, false
	}
	return AICoreName{Provider: provider, Slot: slot}, true
}

type svcAcct struct {
	service string
	account string
}

// AICoreLookup reads ai-core keychain entries read-only (mirrors keychain.py).
func AICoreLookup(ctx context.Context, runner backendSecurityRunner, n AICoreName) ([]byte, error) {
	val, _, err := AICoreLookupWithSource(ctx, runner, n)
	return val, err
}

type backendSecurityRunner interface {
	Run(ctx context.Context, args ...string) ([]byte, error)
}

func servicePrimary(provider string) string {
	return "local.study.ai." + provider
}

func legacyService(provider string) string {
	return "local.study.lecturetranscriber." + provider
}

func accountsForSlot(provider string, slot int) []string {
	if slot == 1 {
		return []string{
			provider + "_api_key",
			provider + "_api_key_1",
		}
	}
	return []string{fmt.Sprintf("%s_api_key_%d", provider, slot)}
}

func legacyAccounts(provider string) []string {
	return []string{
		provider + "_api_key",
		strings.ToUpper(provider) + "_API_KEY",
	}
}

func errorsIsNotFound(err error) bool {
	return kcerrors.Code(err) == kcerrors.ExitNotFound
}

// ListAICoreItems lists provider short names present in dump-keychain output.
func ListAICoreItems(dump string, servicePrefix string) []string {
	if servicePrefix == "" {
		servicePrefix = "local.study.ai"
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(dump, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "service:") {
			continue
		}
		svc := strings.TrimSpace(strings.TrimPrefix(line, "service:"))
		if !strings.HasPrefix(svc, servicePrefix+".") {
			continue
		}
		p := strings.TrimPrefix(svc, servicePrefix+".")
		if _, ok := knownProviders[p]; ok {
			seen[p] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}
