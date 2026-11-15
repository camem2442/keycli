// Package authoritybinding defines the exact metadata shared by Authority IPC
// and the Gateway delivery journal.
package authoritybinding

import "regexp"

var (
	identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	operation  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
	owner      = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	digest     = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Binding is the protected registration projection for one exact intent.
// Operation identifies the registration; MutationID identifies the dispatch.
type Binding struct {
	GoalID           string `json:"goal_id"`
	MutationID       string `json:"mutation_id"`
	WriterGeneration int64  `json:"writer_generation"`
	Owner            string `json:"owner"`
	Session          string `json:"session"`
	Provider         string `json:"provider"`
	Operation        string `json:"operation"`
	ResourceDigest   string `json:"resource_digest"`
	TargetDigest     string `json:"target_digest"`
	PlanDigest       string `json:"plan_digest"`
	GrantDigest      string `json:"grant_digest"`
}

// Valid reports whether every projected field can be represented in the v1 protocol.
func (b Binding) Valid() bool {
	return identifier.MatchString(b.GoalID) && operation.MatchString(b.MutationID) &&
		operation.MatchString(b.Operation) && b.WriterGeneration > 0 &&
		owner.MatchString(b.Owner) && identifier.MatchString(b.Session) &&
		(b.Provider == "cloudflare" || b.Provider == "github" || b.Provider == "oracle") &&
		digest.MatchString(b.ResourceDigest) && digest.MatchString(b.TargetDigest) &&
		digest.MatchString(b.PlanDigest) && digest.MatchString(b.GrantDigest)
}
