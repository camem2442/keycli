// Package secretmeta resolves secret metadata (kind, url, username) across
// overwrites without touching secret values.
package secretmeta

import (
	"context"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
)

// Stored returns the metadata the write backend already holds for name, which
// is the record an overwrite replaces. It reads metadata only, never values.
func Stored(ctx context.Context, a *app.App, name string) (backend.Item, bool, error) {
	return a.Resolver.StoredItem(ctx, name)
}

// StoredAll returns every stored metadata record keyed by lower-case
// canonical name, for callers that overwrite many names at once.
func StoredAll(ctx context.Context, a *app.App) (map[string]backend.Item, error) {
	return a.Resolver.StoredItems(ctx)
}

// Key normalises a name for a StoredAll lookup.
func Key(a *app.App, name string) string {
	return strings.ToLower(a.Resolver.CanonicalName(name))
}

// Preserve keeps the stored classification for writes that do not carry
// one (age restore, interactive wizard). Values the caller set always win, and
// a non-login kind never keeps login-only fields.
func Preserve(prev backend.Item, ok bool, meta backend.Meta) backend.Meta {
	if ok && meta.Kind == "" {
		meta.Kind = prev.Kind
	}
	if meta.Kind != "login" {
		meta.URL = ""
		meta.Username = ""
		return meta
	}
	if ok {
		if meta.URL == "" {
			meta.URL = prev.URL
		}
		if meta.Username == "" {
			meta.Username = prev.Username
		}
	}
	return meta
}

// MergeFlags resolves `kc set` metadata against the stored entry. A changed
// flag wins even when empty, so `--url ""` clears a stale URL; an unchanged
// flag keeps what the entry had, which makes a plain rotation lossless.
func MergeFlags(prev backend.Item, ok bool, meta backend.Meta, kindChanged, urlChanged, usernameChanged bool) backend.Meta {
	if !kindChanged && ok {
		meta.Kind = prev.Kind
	}
	if meta.Kind != "login" {
		meta.URL = ""
		meta.Username = ""
		return meta
	}
	if ok && prev.Kind == "login" {
		if !urlChanged {
			meta.URL = prev.URL
		}
		if !usernameChanged {
			meta.Username = prev.Username
		}
	}
	return meta
}
