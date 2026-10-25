package app

import (
	"context"
	"strings"

	"github.com/camem2442/keycli/internal/ledger"
	"github.com/camem2442/keycli/internal/resolve"
)

// AnnotateStale marks list items that differ from ledger exports.
//
// Only names that appear in the ledger are read, and policy-denied names are
// never read. The ledger is loaded once instead of once per item.
func (a *App) AnnotateStale(ctx context.Context, items []resolve.ListItem) []resolve.ListItem {
	lf := a.LedgerFile()
	entries, err := lf.Load()
	if err != nil || len(entries) == 0 {
		return items
	}
	exported := map[string][]string{}
	for _, e := range entries {
		name := strings.ToLower(strings.TrimSpace(e.Name))
		if name == "" {
			continue
		}
		exported[name] = append(exported[name], e.HMAC)
	}
	if len(exported) == 0 {
		return items
	}
	key, err := a.LedgerHMACKey(ctx)
	if err != nil || len(key) == 0 {
		return items
	}
	for i, it := range items {
		if it.PolicyDenied {
			continue
		}
		hmacs, ok := exported[strings.ToLower(it.Name)]
		if !ok {
			continue
		}
		val, _, err := a.Resolver.Get(ctx, it.Name)
		if err != nil {
			continue
		}
		got := ledger.ComputeHMAC(key, val)
		for _, want := range hmacs {
			if want != got {
				items[i].StaleExport = true
				break
			}
		}
	}
	return items
}
