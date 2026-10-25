package catalog

import "context"

// Reader reads the catalogue: the live entries (retired ones left out), or every entry of a kind.
type Reader interface {
	// Live returns one entry, or ErrNotFound.
	Live(ctx context.Context, kind Kind, key string) (Entry, error)
	// LiveList returns the entries of a kind, by pack then position.
	LiveList(ctx context.Context, kind Kind) ([]Entry, error)
	// List returns every entry of a kind, retired ones included, by pack then position.
	List(ctx context.Context, kind Kind) ([]Entry, error)
}
