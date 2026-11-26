// Package secretmeta resolves secret metadata (kind, url, username) across
// overwrites without touching secret values.
package secretmeta

import (
	"context"
	"strings"

	"github.com/camem2442/keycli/internal/app"
	"github.com/camem2442/keycli/internal/backend"
	"github.com/camem2442/keycli/internal/resolve"
)

// Stored returns the metadata kc already holds for name. It reads list
// metadata only — no value, no policy error — so callers can preserve the
// classification across an overwrite.
func Stored(ctx context.Context, a *app.App, name string) (resolve.ListItem, bool) {
	items, err := a.Resolver.List(ctx)
	if err != nil {
		return resolve.ListItem{}, false
	}
	canon := a.Resolver.CanonicalName(name)
	for _, it := range items {
		if strings.EqualFold(it.Name, canon) {
			return it, true
		}
	}
	return resolve.ListItem{}, false
}

// Preserve keeps the stored classification for writes that do not carry
// one (age restore, interactive wizard). Values the caller set always win, and
// a non-login kind never keeps login-only fields.
func Preserve(prev resolve.ListItem, ok bool, meta backend.Meta) backend.Meta {
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
func MergeFlags(prev resolve.ListItem, ok bool, meta backend.Meta, kindChanged, urlChanged, usernameChanged bool) backend.Meta {
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
