package history

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// Handler reacts to an event after it is stored. It runs in the publisher's goroutine, so it must not wait on the
// publisher; what it publishes in turn reaches every handler again.
type Handler func(ctx context.Context, e platform.Event) error

// Service is the event bus: Publish stores, pushes and dispatches.
type Service struct {
	repo     Repo
	sender   Sender
	clock    platform.Clock
	log      *slog.Logger
	mu       sync.RWMutex
	handlers []Handler
}

// NewService stores events in repo and pushes them through sender.
func NewService(repo Repo, sender Sender, clock platform.Clock, log *slog.Logger) *Service {
	return &Service{repo: repo, sender: sender, clock: clock, log: log}
}

// Subscribe adds a handler for every event published from now on.
func (s *Service) Subscribe(h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, h)
}

// Publish appends e to its user's history (at now, data {} when empty), pushes it to the user's pages, then runs
// the handlers. A handler's error is logged, not returned: the event happened either way.
func (s *Service) Publish(ctx context.Context, e platform.Event) (platform.Event, error) {
	if e.UserID == 0 {
		return e, ErrNoUser
	}
	if e.At.IsZero() {
		e.At = s.clock.Now()
	}
	if len(e.Data) == 0 {
		e.Data = json.RawMessage(`{}`)
	}
	id, err := s.repo.Append(ctx, e)
	if err != nil {
		return e, fmt.Errorf("history %s: %w", e.Kind, err)
	}
	e.ID = id
	doc, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	s.sender.Send(e.UserID, platform.Message{ID: e.ID, Event: StreamEvent, Data: doc})
	s.mu.RLock()
	handlers := append([]Handler(nil), s.handlers...)
	s.mu.RUnlock()
	for _, h := range handlers {
		if err := h(ctx, e); err != nil {
			s.log.Error("event handler failed", slog.String("kind", e.Kind), slog.String("subject", e.Subject),
				slog.Any("error", err))
		}
	}
	return e, nil
}

// Since is user's history after event id `after`, oldest first, at most ReplayLimit events.
func (s *Service) Since(ctx context.Context, user, after int64) ([]platform.Event, error) {
	return s.repo.After(ctx, user, after, ReplayLimit)
}
