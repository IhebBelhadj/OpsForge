package platform

import (
	"encoding/json"
	"sync"
	"time"
)

// Event is one thing that happened to a learner (docs/courses/architecture.md, "Domain events"): appended to the
// history (internal/history), which badge rules read and the event stream replays. Kind is "<module thing>.<what
// happened>" (lab.passed, quiz.submitted, ...; each module declares its kinds), Subject what it happened to (a lab
// id, "<course>/<item>"), Data the details as a JSON object.
type Event struct {
	ID      int64           `json:"id"`
	UserID  int64           `json:"-"`
	Kind    string          `json:"kind"`
	Subject string          `json:"subject"`
	Data    json.RawMessage `json:"data"`
	At      time.Time       `json:"at"`
}

// Message is one server-sent event for a user's open pages: Event names the stream's event type, Data is its JSON
// document. ID is a history event's id (the page sends the last one back as Last-Event-ID when it reconnects);
// 0 for messages the history does not keep (an operation's progress).
type Message struct {
	ID    int64
	Event string
	Data  json.RawMessage
}

// SubscriberBuffer is how many messages a page may fall behind before the hub drops it; the page reconnects and
// the history replays what it missed.
const SubscriberBuffer = 64

// Hub fans messages out to each user's open event streams. Sending never blocks: a stream that fell behind is
// closed instead.
type Hub struct {
	mu     sync.Mutex
	subs   map[int64]map[*Subscription]struct{}
	closed bool
}

// Subscription is one open event stream.
type Subscription struct {
	hub  *Hub
	user int64
	c    chan Message
	once sync.Once
}

// NewHub returns a hub with no subscribers.
func NewHub() *Hub { return &Hub{subs: map[int64]map[*Subscription]struct{}{}} }

// Subscribe opens a stream of user's messages. Its channel closes when the stream falls behind, the hub closes, or
// Close is called.
func (h *Hub) Subscribe(user int64) *Subscription {
	s := &Subscription{hub: h, user: user, c: make(chan Message, SubscriberBuffer)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(s.c)
		return s
	}
	if h.subs[user] == nil {
		h.subs[user] = map[*Subscription]struct{}{}
	}
	h.subs[user][s] = struct{}{}
	return s
}

// Messages is the stream; closed when the subscription ends.
func (s *Subscription) Messages() <-chan Message { return s.c }

// Close ends the subscription.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.dropLocked()
}

func (s *Subscription) dropLocked() {
	s.once.Do(func() {
		delete(s.hub.subs[s.user], s)
		if len(s.hub.subs[s.user]) == 0 {
			delete(s.hub.subs, s.user)
		}
		close(s.c)
	})
}

// Send delivers m to every open stream of user.
func (h *Hub) Send(user int64, m Message) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[user] {
		select {
		case s.c <- m:
		default:
			s.dropLocked()
		}
	}
}

// Close ends every subscription and refuses new ones (shutdown: the event streams would otherwise keep their
// requests open past the grace period).
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, subs := range h.subs {
		for s := range subs {
			s.dropLocked()
		}
	}
}
