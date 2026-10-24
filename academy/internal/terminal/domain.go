// Package terminal runs the browser terminals: one shell on a pty per session, kept alive for a while after its
// WebSocket drops, and replaying its recent output to the client that reconnects (docs/courses/frontend.md
// "Terminal"). The transport (internal/httpapi) frames bytes; this package owns the sessions.
package terminal

import (
	"errors"
	"time"
)

const (
	// ReplayBytes is how much recent output a reconnecting client receives first.
	ReplayBytes = 64 << 10
	// Linger is how long a session survives without a client.
	Linger = 60 * time.Second
	// MaxSessions bounds the shells of the platform (one learner, a few tabs).
	MaxSessions = 8
	// outputQueue chunks may wait for a client; a client further behind is detached (it can reconnect: replay).
	outputQueue = 256
)

var (
	// ErrNotFound is a session that does not exist (never did, or ended).
	ErrNotFound = errors.New("no such terminal session")
	// ErrTooMany is a new session past MaxSessions.
	ErrTooMany = errors.New("too many terminal sessions")
)

// Why an attachment's output ended.
const (
	ReasonExited   = "exited"   // the shell exited; the session is gone
	ReasonReplaced = "replaced" // another client attached to the session
	ReasonSlow     = "slow"     // the client fell behind; the session lingers
	ReasonClosed   = "closed"   // the platform is shutting down
)

// Spec is the shell to start.
type Spec struct {
	Argv []string
	Dir  string
	Env  []string // added to the platform's environment
	Cols uint16
	Rows uint16
}

// ring keeps the last len(buf) bytes written to it.
type ring struct {
	buf  []byte
	next int
	full bool
}

func newRing(size int) *ring { return &ring{buf: make([]byte, size)} }

func (r *ring) Write(p []byte) {
	if len(p) >= len(r.buf) {
		copy(r.buf, p[len(p)-len(r.buf):])
		r.next, r.full = 0, true
		return
	}
	n := copy(r.buf[r.next:], p)
	if n < len(p) {
		copy(r.buf, p[n:])
		r.full = true
	}
	r.next = (r.next + len(p)) % len(r.buf)
	if r.next == 0 && len(p) > 0 {
		r.full = true
	}
}

// Bytes is the kept output, oldest first (a copy).
func (r *ring) Bytes() []byte {
	if !r.full {
		return append([]byte(nil), r.buf[:r.next]...)
	}
	return append(append([]byte(nil), r.buf[r.next:]...), r.buf[:r.next]...)
}
