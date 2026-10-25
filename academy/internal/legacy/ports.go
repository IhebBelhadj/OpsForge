package legacy

import "context"

// Reader reads a Phase 10 database.
type Reader interface {
	Read(ctx context.Context, path string) (Source, error)
}

// Store is the platform's database as the migration writes it.
type Store interface {
	// Done is what earlier runs imported.
	Done(ctx context.Context) (Done, error)
	// Apply writes the plan for user in one transaction (nothing on error).
	Apply(ctx context.Context, user int64, p Plan) (Report, error)
}
