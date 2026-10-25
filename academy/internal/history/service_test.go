package history_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/history"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
)

type clock struct{ now time.Time }

func (c clock) Now() time.Time { return c.now }

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

type memRepo struct {
	mu     sync.Mutex
	events []platform.Event
	fail   error
}

func (r *memRepo) Append(_ context.Context, e platform.Event) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return 0, r.fail
	}
	e.ID = int64(len(r.events) + 1)
	r.events = append(r.events, e)
	return e.ID, nil
}

func (r *memRepo) After(_ context.Context, user, after int64, limit int) ([]platform.Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []platform.Event
	for _, e := range r.events {
		if e.UserID == user && e.ID > after && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

type sent struct {
	user int64
	msg  platform.Message
}

type recorder struct{ sent []sent }

func (r *recorder) Send(user int64, m platform.Message) { r.sent = append(r.sent, sent{user, m}) }

func newService() (*history.Service, *memRepo, *recorder) {
	repo, rec := &memRepo{}, &recorder{}
	return history.NewService(repo, rec, clock{t0}, slog.New(slog.NewTextHandler(io.Discard, nil))), repo, rec
}

func TestPublishStoresPushesAndDispatches(t *testing.T) {
	svc, repo, rec := newService()
	var handled []string
	svc.Subscribe(func(_ context.Context, e platform.Event) error {
		handled = append(handled, e.Kind)
		return nil
	})
	e, err := svc.Publish(context.Background(), platform.Event{UserID: 3, Kind: "lab.started", Subject: "meet"})
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != 1 || !e.At.Equal(t0) || string(e.Data) != "{}" {
		t.Fatalf("published %+v", e)
	}
	if len(repo.events) != 1 || repo.events[0].Kind != "lab.started" {
		t.Fatalf("stored %+v", repo.events)
	}
	if len(rec.sent) != 1 || rec.sent[0].user != 3 || rec.sent[0].msg.ID != 1 ||
		rec.sent[0].msg.Event != history.StreamEvent {
		t.Fatalf("pushed %+v", rec.sent)
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.sent[0].msg.Data, &doc); err != nil || doc["kind"] != "lab.started" ||
		doc["subject"] != "meet" || doc["id"] != 1.0 || doc["user_id"] != nil {
		t.Fatalf("pushed document %s (%v)", rec.sent[0].msg.Data, err)
	}
	if len(handled) != 1 {
		t.Fatalf("handlers saw %v", handled)
	}
}

func TestAHandlerMayPublishAndItsErrorIsNotThePublishers(t *testing.T) {
	svc, repo, _ := newService()
	svc.Subscribe(func(ctx context.Context, e platform.Event) error {
		if e.Kind == "lab.passed" {
			_, err := svc.Publish(ctx, platform.Event{UserID: e.UserID, Kind: "course.completed", Subject: "c"})
			return err
		}
		return errors.New("not interested")
	})
	if _, err := svc.Publish(context.Background(), platform.Event{UserID: 1, Kind: "lab.passed"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.events) != 2 || repo.events[1].Kind != "course.completed" {
		t.Fatalf("events %+v", repo.events)
	}
}

func TestPublishRefusesWhatCannotBeStored(t *testing.T) {
	svc, repo, rec := newService()
	if _, err := svc.Publish(context.Background(), platform.Event{Kind: "x"}); !errors.Is(err, history.ErrNoUser) {
		t.Fatalf("no user: %v", err)
	}
	repo.fail = errors.New("disk full")
	if _, err := svc.Publish(context.Background(), platform.Event{UserID: 1, Kind: "x"}); err == nil {
		t.Fatal("a failed append was not reported")
	}
	if len(rec.sent) != 0 {
		t.Fatal("an event that was not stored was pushed")
	}
}

func TestSinceReplaysTheUsersLaterEvents(t *testing.T) {
	svc, _, _ := newService()
	ctx := context.Background()
	for _, u := range []int64{1, 2, 1, 1} {
		if _, err := svc.Publish(ctx, platform.Event{UserID: u, Kind: "k"}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := svc.Since(ctx, 1, 1)
	if err != nil || len(got) != 2 || got[0].ID != 3 || got[1].ID != 4 {
		t.Fatalf("%+v %v", got, err)
	}
}
