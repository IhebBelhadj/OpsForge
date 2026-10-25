package catalog

import "context"

// Reader reads the live catalogue (retired entries are left out).
type Reader interface {
	// Live returns one entry, or ErrNotFound.
	Live(ctx context.Context, kind Kind, key string) (Entry, error)
	// LiveList returns the entries of a kind, by pack then position.
	LiveList(ctx context.Context, kind Kind) ([]Entry, error)
}
