// Package platform is the shared kernel every module may use: build information, the clock and logging.
// It imports nothing from the modules or the transport.
package platform

import (
	"io"
	"log/slog"
	"time"
)

// Build identifies the running binary (stamped with -ldflags by make academy).
type Build struct {
	Version string
}

// Clock is the time source; services take it as a port so tests can fix the time.
type Clock interface {
	Now() time.Time
}

// SystemClock is the real clock, in UTC.
type SystemClock struct{}

// Now returns the current time in UTC.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// NewLogger writes JSON log lines at `level` to w.
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}))
}
