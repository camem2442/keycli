package app

import (
	"context"

	"github.com/camem2442/keycli/internal/backend/file"
	"github.com/camem2442/keycli/internal/ledger"
)

// LedgerHMACKey unlocks the file store and returns the ledger HMAC key.
func (a *App) LedgerHMACKey(ctx context.Context) ([]byte, error) {
	store, err := a.FileStore(ctx)
	if err != nil {
		return nil, err
	}
	return file.OpenStoreForLedger(store)
}

// LedgerFile returns the ledger at ~/.kc/ledger.json.
func (a *App) LedgerFile() ledger.File {
	return ledger.File{Path: ledger.Path(a.Home)}
}

// FileStore returns the configured file backend instance.
func (a *App) FileStore(ctx context.Context) (*file.Store, error) {
	fs := a.Resolver.FileStore()
	if fs == nil {
		return nil, file.ErrNoStoreIdentity
	}
	return fs, nil
}
