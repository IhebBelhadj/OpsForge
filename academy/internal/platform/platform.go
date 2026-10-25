// Package platform is the shared kernel every module may use: build information, the clock, randomness, logging
// and the domain events with their fan-out to the pages (events.go).
// It imports nothing from the modules or the transport.
package platform

import (
	"crypto/rand"
	"io"
	"log/slog"
	"math/big"
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

// Random draws permutations (quiz draws and shuffles); services take it as a port so tests can fix the order.
type Random interface {
	// Perm is a random permutation of 0..n-1.
	Perm(n int) []int
}

// CryptoRandom is Random from crypto/rand: a learner cannot predict the next draw.
type CryptoRandom struct{}

// Perm shuffles 0..n-1 (Fisher-Yates).
func (CryptoRandom) Perm(n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	for i := n - 1; i > 0; i-- {
		j, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1))) // never fails (crypto/rand panics instead)
		k := int(j.Int64())
		p[i], p[k] = p[k], p[i]
	}
	return p
}
