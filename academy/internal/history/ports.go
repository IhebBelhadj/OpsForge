package history

import (
	"context"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Repo appends to and reads the history.
type Repo interface {
	// Append stores e and returns its id.
	Append(ctx context.Context, e platform.Event) (int64, error)
	// After returns user's events with an id above after, oldest first, at most limit.
	After(ctx context.Context, user, after int64, limit int) ([]platform.Event, error)
}

// Sender pushes a message to a user's open pages (platform.Hub).
type Sender interface {
	Send(user int64, m platform.Message)
}
