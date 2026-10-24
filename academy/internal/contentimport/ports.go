package contentimport

import (
	"context"
	"encoding/json"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
)

// Compiler compiles the repository's packs into a bundle checked against bundle.schema.json.
type Compiler interface {
	Compile(ctx context.Context) (json.RawMessage, error)
}

// Store is the catalogue's side of an import.
type Store interface {
	// Entries returns every entry, retired ones included.
	Entries(ctx context.Context) ([]catalog.Entry, error)
	// Apply writes upsert (un-retiring them) and retires retire, at `at`, in one transaction.
	Apply(ctx context.Context, upsert, retire []catalog.Entry, at time.Time) error
}
