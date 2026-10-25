package quiz_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/quiz"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type memRepo struct {
	mu       sync.Mutex
	attempts []quiz.Attempt
}

func (r *memRepo) Create(_ context.Context, a quiz.Attempt) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a.ID = int64(len(r.attempts) + 1)
	r.attempts = append(r.attempts, a)
	return a.ID, nil
}

func (r *memRepo) Attempt(_ context.Context, id int64) (quiz.Attempt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id < 1 || int(id) > len(r.attempts) {
		return quiz.Attempt{}, quiz.ErrAttemptNotFound
	}
	return r.attempts[id-1], nil
}

func (r *memRepo) Submit(_ context.Context, a quiz.Attempt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.attempts[a.ID-1].SubmittedAt != nil {
		return quiz.ErrSubmitted
	}
	r.attempts[a.ID-1] = a
	return nil
}

func (r *memRepo) Attempts(_ context.Context, user int64, course, item string) ([]quiz.Attempt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []quiz.Attempt
	for _, a := range slices.Backward(r.attempts) {
		if a.UserID == user && a.Course == course && a.Item == item {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r *memRepo) Passed(_ context.Context, user int64) ([]quiz.Attempt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []quiz.Attempt
	for _, a := range r.attempts {
		if a.UserID == user && a.Passed {
			out = append(out, a)
		}
	}
	return out, nil
}

// content serves the golden quiz as k8s-intro/pods-check (time limited when limit is set) and a lecture.
type content struct {
	doc   json.RawMessage
	limit *int
}

func (c content) Content(_ context.Context, course, id string) (catalog.ItemRef, catalog.Entry, error) {
	if course != "k8s-intro" {
		return catalog.ItemRef{}, catalog.Entry{}, catalog.ErrNotFound
	}
	switch id {
	case "pods-check":
		doc := c.doc
		if c.limit != nil {
			var m map[string]any
			_ = json.Unmarshal(doc, &m)
			m["time_limit_s"] = *c.limit
			doc, _ = json.Marshal(m)
		}
		return catalog.ItemRef{ID: id, Type: catalog.ItemQuiz, Hash: "Q1"}, catalog.Entry{Kind: catalog.KindQuiz, Doc: doc}, nil
	case "what-is-a-pod":
		return catalog.ItemRef{ID: id, Type: catalog.ItemLecture}, catalog.Entry{Kind: catalog.KindLecture}, nil
	}
	return catalog.ItemRef{}, catalog.Entry{}, catalog.ErrNotFound
}

type bus struct{ events []platform.Event }

func (b *bus) Publish(_ context.Context, e platform.Event) (platform.Event, error) {
	b.events = append(b.events, e)
	return e, nil
}

type fixture struct {
	svc   *quiz.Service
	repo  *memRepo
	bus   *bus
	clock *clock
}

func newFixture(t *testing.T, limit *int) fixture {
	repo, b, c := &memRepo{}, &bus{}, &clock{now: t0}
	return fixture{svc: quiz.NewService(repo, content{goldenQuiz(t), limit}, b, reverse{}, c), repo: repo, bus: b,
		clock: c}
}

// right answers every question of pods-check correctly.
var right = map[string][]int{"shared": {1}, "phases": {0, 1, 3}}

func TestStartDrawsTheQuestions(t *testing.T) {
	f := newFixture(t, nil)
	a, err := f.svc.Start(context.Background(), 1, "k8s-intro", "pods-check")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != 1 || a.Hash != "Q1" || a.Deadline != nil || a.Pass != 70 || a.Offered != 20 || ids(a.Questions) != "phasesshared" ||
		!a.StartedAt.Equal(t0) {
		t.Fatalf("%+v", a)
	}
	for _, tc := range []struct {
		course, item string
		want         error
	}{
		{"k8s-intro", "what-is-a-pod", quiz.ErrNotAQuiz},
		{"k8s-intro", "nope", catalog.ErrNotFound},
		{"nope", "pods-check", catalog.ErrNotFound},
	} {
		if _, err := f.svc.Start(context.Background(), 1, tc.course, tc.item); !errors.Is(err, tc.want) {
			t.Errorf("%s/%s: %v", tc.course, tc.item, err)
		}
	}
}

func TestSubmitScoresAndPaysXPOnTheFirstPassOnly(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()
	submit := func(answers map[string][]int) quiz.Attempt {
		t.Helper()
		a, _ := f.svc.Start(ctx, 1, "k8s-intro", "pods-check")
		got, err := f.svc.Submit(ctx, 1, a.ID, answers)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	failed := submit(map[string][]int{"shared": {1}})
	if failed.Passed || failed.Score != 50 || failed.XP != 0 || !failed.SubmittedAt.Equal(t0) {
		t.Fatalf("failed attempt %+v", failed)
	}
	first := submit(right)
	if !first.Passed || first.Score != 100 || first.XP != 20 {
		t.Fatalf("first pass %+v", first)
	}
	if second := submit(right); !second.Passed || second.XP != 0 {
		t.Fatalf("second pass %+v", second)
	}
	if len(f.bus.events) != 3 || f.bus.events[1].Kind != quiz.KindSubmitted ||
		f.bus.events[1].Subject != "k8s-intro/pods-check" {
		t.Fatalf("events %+v", f.bus.events)
	}
	var data map[string]any
	_ = json.Unmarshal(f.bus.events[1].Data, &data)
	if data["passed"] != true || data["xp"] != 20.0 || data["score"] != 100.0 || data["content_hash"] != "Q1" {
		t.Fatalf("event data %s", f.bus.events[1].Data)
	}
	list, err := f.svc.Attempts(ctx, 1, "k8s-intro", "pods-check")
	if err != nil || len(list) != 3 || list[0].ID != 3 {
		t.Fatalf("%+v %v", list, err)
	}
	if passed, _ := f.svc.Passed(ctx, 1); len(passed) != 2 {
		t.Fatalf("passed %+v", passed)
	}
}

func TestSubmitRefusals(t *testing.T) {
	limit := 60
	f := newFixture(t, &limit)
	ctx := context.Background()
	a, _ := f.svc.Start(ctx, 1, "k8s-intro", "pods-check")
	if a.Deadline == nil || !a.Deadline.Equal(t0.Add(time.Minute)) {
		t.Fatalf("deadline %v", a.Deadline)
	}
	if _, err := f.svc.Submit(ctx, 2, a.ID, right); !errors.Is(err, quiz.ErrAttemptNotFound) {
		t.Fatalf("another user's attempt: %v", err)
	}
	if _, err := f.svc.Submit(ctx, 1, 99, right); !errors.Is(err, quiz.ErrAttemptNotFound) {
		t.Fatalf("unknown attempt: %v", err)
	}
	if _, err := f.svc.Submit(ctx, 1, a.ID, map[string][]int{"nope": {0}}); !errors.Is(err, quiz.ErrInvalidAnswers) {
		t.Fatalf("invalid answers: %v", err)
	}
	f.clock.now = t0.Add(time.Minute + quiz.DeadlineGrace)
	if _, err := f.svc.Submit(ctx, 1, a.ID, right); err != nil {
		t.Fatalf("within the grace: %v", err)
	}
	if _, err := f.svc.Submit(ctx, 1, a.ID, right); !errors.Is(err, quiz.ErrSubmitted) {
		t.Fatalf("twice: %v", err)
	}
	late, _ := f.svc.Start(ctx, 1, "k8s-intro", "pods-check")
	f.clock.now = f.clock.now.Add(time.Minute + quiz.DeadlineGrace + time.Second)
	if _, err := f.svc.Submit(ctx, 1, late.ID, right); !errors.Is(err, quiz.ErrExpired) {
		t.Fatalf("past the deadline: %v", err)
	}
	if len(f.bus.events) != 1 {
		t.Fatalf("refused submissions published: %+v", f.bus.events)
	}
	if _, err := f.svc.Attempt(ctx, 2, a.ID); !errors.Is(err, quiz.ErrAttemptNotFound) {
		t.Fatalf("reading another user's attempt: %v", err)
	}
	if _, err := f.svc.Attempts(ctx, 1, "k8s-intro", "what-is-a-pod"); !errors.Is(err, quiz.ErrNotAQuiz) {
		t.Fatalf("attempts of a lecture: %v", err)
	}
}
