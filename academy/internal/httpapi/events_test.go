package httpapi_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

// stream is an open GET /api/v1/events.
type stream struct {
	resp   *http.Response
	lines  chan string
	cancel context.CancelFunc
	raw    strings.Builder
}

func openStream(t *testing.T, b *browser, query string, header http.Header) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, b.srv.URL+"/api/v1/events"+query, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := b.client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s := &stream{resp: resp, lines: make(chan string, 256), cancel: cancel}
	go func() {
		defer close(s.lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
	}()
	t.Cleanup(func() {
		cancel()
		_ = resp.Body.Close()
	})
	return s
}

// next reads the next event (comments skipped); it fails after 5 s.
func (s *stream) next(t *testing.T) sseEvent {
	t.Helper()
	var block []string
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				t.Fatal("the stream ended")
			}
			s.raw.WriteString(line + "\n")
			if line != "" {
				block = append(block, line)
				continue
			}
			if es := parseEvents(strings.Join(block, "\n")); len(es) == 1 {
				return es[0]
			}
			block = nil
		case <-time.After(5 * time.Second):
			t.Fatal("no event within 5 s")
		}
	}
}

// check validates what the stream sent so far against the contract.
func (s *stream) check(t *testing.T) {
	t.Helper()
	rec := httptest.NewRecorder()
	for k, v := range s.resp.Header {
		rec.Header()[k] = v
	}
	rec.WriteHeader(s.resp.StatusCode)
	rec.Body.WriteString(s.raw.String())
	checkContract(t, "GET", "/api/v1/events", rec)
}

func TestTheEventStreamPushesTheUsersHistory(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	s := openStream(t, b, "", nil)
	if s.resp.StatusCode != 200 || s.resp.Header.Get("Content-Type") != "text/event-stream" ||
		s.resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("%d %v", s.resp.StatusCode, s.resp.Header)
	}
	ctx := context.Background()
	if _, err := w.history.Publish(ctx, platform.Event{UserID: 1, Kind: "lab.started", Subject: "meet"}); err != nil {
		t.Fatal(err)
	}
	e := s.next(t)
	if e.id != "1" || e.event != "history" || !strings.Contains(e.data, `"kind":"lab.started"`) {
		t.Fatalf("got %+v", e)
	}
	s.check(t)
}

func TestTheEventStreamReplaysWhatAReconnectingPageMissed(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	ctx := context.Background()
	for _, kind := range []string{"a", "b", "c"} {
		if _, err := w.history.Publish(ctx, platform.Event{UserID: 1, Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}
	for name, open := range map[string]func() *stream{
		"header": func() *stream { return openStream(t, b, "", http.Header{"Last-Event-Id": {"1"}}) },
		"query":  func() *stream { return openStream(t, b, "?last_event_id=1", nil) },
	} {
		t.Run(name, func(t *testing.T) {
			s := open()
			if e := s.next(t); e.id != "2" {
				t.Fatalf("first replayed %+v", e)
			}
			if e := s.next(t); e.id != "3" {
				t.Fatalf("second replayed %+v", e)
			}
			s.check(t)
		})
	}
}

func TestTheEventStreamNeedsASession(t *testing.T) {
	w := newWorld(t)
	srv := apiServer(t, w.deps)
	anon := &browser{srv: srv, client: http.DefaultClient}
	rec := record(anon.send(t, http.MethodGet, "/api/v1/events", "", nil))
	if rec.Code != 401 {
		t.Fatalf("%d", rec.Code)
	}
	checkContract(t, "GET", "/api/v1/events", rec)
}

func TestTheEventStreamEndsWhenTheHubCloses(t *testing.T) {
	w := newWorld(t)
	b := signIn(t, apiServer(t, w.deps))
	s := openStream(t, b, "", nil)
	<-s.lines // "retry: ..." : the handler is subscribed
	w.hub.Close()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-s.lines:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the stream stayed open after the hub closed")
		}
	}
}
