package operations

import (
	"context"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Repo stores operations.
type Repo interface {
	Create(ctx context.Context, op Operation) error
	// Finish records a finished operation's status, result and error.
	Finish(ctx context.Context, op Operation) error
	// Get reads an operation, or ErrNotFound.
	Get(ctx context.Context, id string) (Operation, error)
	// FailRunning marks every running operation failed with reason (at start: they belonged to a stopped process).
	FailRunning(ctx context.Context, reason string, at time.Time) (int64, error)
}

// Sender pushes a message to a user's open pages (platform.Hub).
type Sender interface {
	Send(user int64, m platform.Message)
}
