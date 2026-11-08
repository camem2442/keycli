package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/camem2442/keycli/internal/config"
)

// EffectivePolicyDigest binds the selected agent's policy and the entire alias
// table, since alias changes can affect policy decisions. It never reads a
// secret backend or includes credential values. This is an identity fingerprint,
// not a security boundary or an authenticated verification receipt.
func EffectivePolicyDigest(cfg config.Config, agent string) (string, error) {
	if err := cfg.ValidatePolicyPatterns(); err != nil {
		return "", err
	}
	if agent == "" {
		agent = DetectAgent()
	}
	spec, source := EffectiveRules(cfg, agent)
	aliases := make(map[string][]string, len(cfg.Alias))
	for canonical, names := range cfg.Alias {
		aliases[canonical] = append([]string{}, names...)
	}
	// Keep these field names and their order stable for v1. encoding/json
	// sorts map keys; list order is significant. Empty lists are always [].
	payload := struct {
		V        int                 `json:"v"`
		Kind     string              `json:"kind"`
		Agent    string              `json:"agent"`
		Enforced bool                `json:"enforced"`
		Source   string              `json:"source"`
		Allow    []string            `json:"allow"`
		Deny     []string            `json:"deny"`
		Aliases  map[string][]string `json:"aliases"`
	}{
		V:        1,
		Kind:     "keycli-effective-agent-policy",
		Agent:    agent,
		Enforced: PolicyEnabled(cfg),
		Source:   source,
		Allow:    append([]string{}, spec.Allow...),
		Deny:     append([]string{}, spec.Deny...),
		Aliases:  aliases,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
