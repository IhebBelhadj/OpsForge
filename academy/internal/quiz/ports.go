package quiz

import (
	"context"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Repo stores attempts.
type Repo interface {
	// Create stores a new attempt and returns its id.
	Create(ctx context.Context, a Attempt) (int64, error)
	// Attempt reads one attempt, or ErrAttemptNotFound.
	Attempt(ctx context.Context, id int64) (Attempt, error)
	// Submit records a graded attempt; ErrSubmitted if it already was.
	Submit(ctx context.Context, a Attempt) error
	// Attempts are user's attempts at an item, newest first.
	Attempts(ctx context.Context, user int64, course, item string) ([]Attempt, error)
	// Passed are user's passed attempts, oldest first.
	Passed(ctx context.Context, user int64) ([]Attempt, error)
}

// Content reads an item with the entry holding its keys (catalog.Service).
type Content interface {
	Content(ctx context.Context, course, id string) (catalog.ItemRef, catalog.Entry, error)
}

// Publisher appends to the history (history.Service).
type Publisher interface {
	Publish(ctx context.Context, e platform.Event) (platform.Event, error)
}
