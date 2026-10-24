package identity

import (
	"context"
	"time"
)

// Repo stores users and sessions.
type Repo interface {
	// EnsureUser returns the user called name, creating it (at now) if there is none.
	EnsureUser(ctx context.Context, name string, now time.Time) (User, error)
	User(ctx context.Context, id int64) (User, error)
	CreateSession(ctx context.Context, s Session) error
	// Session returns the session with this token hash, or ErrNoSession.
	Session(ctx context.Context, tokenHash string) (Session, error)
	// DeleteExpired removes the sessions that expired before now.
	DeleteExpired(ctx context.Context, now time.Time) error
}
