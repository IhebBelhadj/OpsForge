// Package operations runs the platform's long operations (docs/courses/architecture.md, "HTTP API"): a request
// that sets up a lab, runs a check or tears one down answers 202 with an operation; the work runs in the
// background, its new status is pushed on the event stream (`operation`) and GET /api/v1/operations/{id} reads it
// after a reconnect. The result is the module's own document; a failed operation is an internal error, the
// domain's failures (a setup that failed) are in the result.
package operations

import (
	"encoding/json"
	"errors"
	"time"
)

// StreamEvent is the event-stream type of an operation's new status.
const StreamEvent = "operation"

// Status is where an operation is.
type Status string

// The statuses of an operation.
const (
	Running Status = "running"
	Done    Status = "done"   // finished; the result says how it went
	Failed  Status = "failed" // could not finish (an internal error, a restart)
)

// Interrupted is the error of the operations a restart cut short.
const Interrupted = "interrupted: the platform stopped before the operation finished"

// ErrNotFound is an operation that does not exist or is another user's.
var ErrNotFound = errors.New("no such operation")

// Operation is one long operation; its JSON is the API's Operation document.
type Operation struct {
	ID         string          `json:"id"`
	UserID     int64           `json:"-"`
	Kind       string          `json:"kind"`
	Subject    string          `json:"subject"`
	Status     Status          `json:"status"`
	Result     json.RawMessage `json:"result"`
	Error      *string         `json:"error"`
	CreatedAt  time.Time       `json:"created_at"`
	FinishedAt *time.Time      `json:"finished_at"`
}
