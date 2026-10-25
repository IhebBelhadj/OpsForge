// Package history is the learner's history: every domain event (docs/courses/architecture.md, "Domain events")
// is appended to the events table, pushed to the user's open pages (the event stream) and handed to the in-process
// handlers that react to it (progress notices a completed course). Badge rules read it; nothing rewrites it.
package history

import "errors"

// StreamEvent is the event-stream type of a history event (the page refreshes what the kind concerns).
const StreamEvent = "history"

// ReplayLimit caps the events replayed to a page that reconnects; one further behind reloads instead.
const ReplayLimit = 500

// ErrNoUser is an event without a user.
var ErrNoUser = errors.New("an event needs a user")
