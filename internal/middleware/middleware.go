package middleware

import "context"

// Item is metadata for a secret name (no value).
type Item struct {
	Name    string
	Backend string
	Note    string
	Expires *string
}

// Lookup resolves a secret name to its raw value.
type Lookup func(ctx context.Context, name string) ([]byte, string, error)

// Middleware wraps a Lookup chain (implementation DEFERRED).
type Middleware func(next Lookup) Lookup
