package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/history"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// History is what the transport needs from the history module.
type History interface {
	Since(ctx context.Context, user, after int64) ([]platform.Event, error)
}

// Streams opens a user's live event stream (platform.Hub).
type Streams interface {
	Subscribe(user int64) *platform.Subscription
}

// heartbeat is how often an idle stream sends a comment line, so proxies and the browser keep it open.
const heartbeat = 25 * time.Second

// streamEvents is GET /api/v1/events (openapi.yaml streamEvents): server-sent events. The subscription opens
// before the replay, so nothing published in between is lost; replayed ids are not sent twice.
func (s *server) streamEvents(w http.ResponseWriter, r *http.Request) {
	p, ok := principal(r.Context())
	if !ok {
		writeProblem(w, http.StatusInternalServerError, "/problems/internal", "Internal error", "")
		return
	}
	after, replay := lastEventID(r)
	sub := s.deps.Streams.Subscribe(p.User.ID)
	defer sub.Close()
	var past []platform.Event
	if replay {
		var err error
		if past, err = s.deps.History.Since(r.Context(), p.User.ID, after); err != nil {
			writeProblem(w, http.StatusInternalServerError, "/problems/internal", "Internal error", "")
			return
		}
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	write := func(format string, args ...any) error {
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return err
		}
		return rc.Flush()
	}
	if write("retry: 2000\n: connected\n\n") != nil {
		return
	}
	for _, e := range past {
		doc, err := json.Marshal(e)
		if err != nil || write("id: %d\nevent: %s\ndata: %s\n\n", e.ID, history.StreamEvent, doc) != nil {
			return
		}
		after = e.ID
	}
	tick := time.NewTicker(heartbeat)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			if write(": ping\n\n") != nil {
				return
			}
		case m, open := <-sub.Messages():
			if !open { // fell behind, or the server is shutting down: the browser reconnects
				return
			}
			var err error
			switch {
			case m.ID == 0:
				err = write("event: %s\ndata: %s\n\n", m.Event, m.Data)
			case m.ID > after:
				err = write("id: %d\nevent: %s\ndata: %s\n\n", m.ID, m.Event, m.Data)
			}
			if err != nil {
				return
			}
		}
	}
}

// lastEventID is the id to replay after (Last-Event-ID, else ?last_event_id); false when neither is set.
func lastEventID(r *http.Request) (int64, bool) {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("last_event_id")
	}
	if v == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id < 0 {
		return 0, false
	}
	return id, true
}
