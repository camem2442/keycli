package app

import (
	"context"

	"github.com/camem2442/keycli/internal/resolve"
)

// AnnotateStale marks list items that differ from ledger exports.
func (a *App) AnnotateStale(ctx context.Context, items []resolve.ListItem) []resolve.ListItem {
	key, err := a.LedgerHMACKey(ctx)
	if err != nil {
		return items
	}
	lf := a.LedgerFile()
	for i, it := range items {
		val, _, err := a.Resolver.Get(ctx, it.Name)
		if err != nil {
			continue
		}
		if lf.StaleAnyTarget(key, it.Name, val) {
			items[i].StaleExport = true
		}
	}
	return items
}
