// Package identity is who is using the platform: the local user (one for now, created at first use) and the
// browser sessions that carry it (an HttpOnly cookie) with their CSRF token (docs/courses/architecture.md,
// "Security"). A multi-user mode adds real sign-in in front of OpenSession; the rest stays.
package identity

import (
	"errors"
	"time"
)

// LocalUser is the name of the one local user (Phase 10's labs.db uses the same, for migrate-legacy).
const LocalUser = "learner"

// SessionTTL is how long a session lasts; the browser then opens a new one without the learner noticing.
const SessionTTL = 30 * 24 * time.Hour

var (
	// ErrNoSession is a request without a valid session: no cookie, an unknown or expired token.
	ErrNoSession = errors.New("no valid session")
	// ErrBadCSRF is a state-changing request whose CSRF token is missing or not the session's.
	ErrBadCSRF = errors.New("missing or wrong CSRF token")
)

// User is a learner.
type User struct {
	ID        int64
	Name      string
	CreatedAt time.Time
}

// Session is a signed-in browser. The token is the cookie's value; only its hash is stored.
type Session struct {
	TokenHash string
	UserID    int64
	CSRF      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Principal is the user behind a request and the session it came with.
type Principal struct {
	User    User
	Session Session
}
