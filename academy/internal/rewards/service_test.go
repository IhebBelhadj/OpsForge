package rewards_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/rewards"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type memRepo map[int64]map[string]time.Time

func (r memRepo) Awards(_ context.Context, user int64) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for k, v := range r[user] {
		out[k] = v
	}
	return out, nil
}

func (r memRepo) Award(_ context.Context, user int64, badge string, at time.Time) (bool, error) {
	if r[user] == nil {
		r[user] = map[string]time.Time{}
	}
	if _, ok := r[user][badge]; ok {
		return false, nil
	}
	r[user][badge] = at
	return true, nil
}

type memCatalog []catalog.Entry

func (c *memCatalog) Entries(_ context.Context, kind catalog.Kind) ([]catalog.Entry, error) {
	var out []catalog.Entry
	for _, e := range *c {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out, nil
}

type events []platform.Event

func (e *events) Publish(_ context.Context, ev platform.Event) (platform.Event, error) {
	*e = append(*e, ev)
	return ev, nil
}

func badge(id, rule string, pos int) catalog.Entry {
	return catalog.Entry{Kind: catalog.KindBadge, Key: id, PackID: "core", Position: pos, Title: id,
		Doc: json.RawMessage(`{"id":"` + id + `","description":"d","icon":null,"rule":` + rule + `}`)}
}

// world is the core pack's curricula, a course awarding "intro-done", and three badges.
type world struct {
	catalog memCatalog
	repo    memRepo
	events  events
	labs    map[int64][]rewards.LabPass
	quizzes map[int64][]rewards.QuizPass
	courses map[int64][]string
	svc     *rewards.Service
}

func newWorld() *world {
	w := &world{
		catalog: memCatalog{
			{Kind: catalog.KindPack, Key: "core", PackID: "core", Doc: json.RawMessage(
				`{"id":"core","curricula":[{"id":"cloud","levels":[{"labs":["a"]},{"labs":["b"]}]}]}`)},
			{Kind: catalog.KindCourse, Key: "k8s-intro", PackID: "core", Doc: json.RawMessage(`{"badge":"intro-done"}`)},
			badge("first", `{"type":"labs_passed","min":1}`, 0),
			badge("cloud", `{"type":"curriculum_complete","curriculum":"cloud"}`, 1),
			badge("intro-done", `{"type":"xp","min":100000}`, 2),
			badge("old", `{"type":"labs_passed","min":0}`, 3),
		},
		repo: memRepo{}, labs: map[int64][]rewards.LabPass{}, quizzes: map[int64][]rewards.QuizPass{},
		courses: map[int64][]string{},
	}
	w.catalog[len(w.catalog)-1].Retired = true
	sources := rewards.Sources{
		Labs:    func(_ context.Context, u int64) ([]rewards.LabPass, error) { return w.labs[u], nil },
		Quizzes: func(_ context.Context, u int64) ([]rewards.QuizPass, error) { return w.quizzes[u], nil },
		Courses: func(_ context.Context, u int64) ([]string, error) { return w.courses[u], nil },
		Users:   func(context.Context) ([]int64, error) { return []int64{1, 2}, nil },
	}
	w.svc = rewards.NewService(w.repo, &w.catalog, sources, &w.events, &clock{t0},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	return w
}

func ids(awards []rewards.Award) []string {
	out := []string{}
	for _, a := range awards {
		out = append(out, a.ID)
	}
	return out
}

func TestEvaluateAwardsOnceAndAnnounces(t *testing.T) {
	w, ctx := newWorld(), context.Background()
	if got, err := w.svc.Evaluate(ctx, 1); err != nil || len(got) != 0 {
		t.Fatalf("nothing done: %v, %v", ids(got), err)
	}
	w.labs[1] = []rewards.LabPass{{Lab: "a", XP: 100}}
	got, err := w.svc.Evaluate(ctx, 1)
	if err != nil || len(got) != 1 || got[0].ID != "first" {
		t.Fatalf("first pass: %v, %v", ids(got), err)
	}
	if len(w.events) != 1 || w.events[0].Kind != rewards.KindAwarded || w.events[0].Subject != "first" ||
		w.events[0].UserID != 1 {
		t.Fatalf("events = %+v", w.events)
	}
	if got, _ := w.svc.Evaluate(ctx, 1); len(got) != 0 || len(w.events) != 1 {
		t.Fatalf("awarded twice: %v", ids(got))
	}
	w.labs[1] = append(w.labs[1], rewards.LabPass{Lab: "b", XP: 100})
	w.courses[1] = []string{"k8s-intro"}
	if got, _ := w.svc.Evaluate(ctx, 1); len(ids(got)) != 2 || got[0].ID != "cloud" || got[1].ID != "intro-done" {
		t.Fatalf("curriculum and course: %v", ids(got))
	}
}

func TestOnEventEvaluatesAfterPasses(t *testing.T) {
	w, ctx := newWorld(), context.Background()
	w.labs[1] = []rewards.LabPass{{Lab: "a", XP: 100}}
	if err := w.svc.OnEvent(ctx, platform.Event{UserID: 1, Kind: "step.checked"}); err != nil || len(w.events) != 0 {
		t.Fatalf("evaluated after a step: %v, %d", err, len(w.events))
	}
	if err := w.svc.OnEvent(ctx, platform.Event{UserID: 1, Kind: "lab.passed"}); err != nil || len(w.events) != 1 {
		t.Fatalf("not evaluated after a pass: %v, %d", err, len(w.events))
	}
}

func TestEvaluateAllIsRetroactive(t *testing.T) {
	w, ctx := newWorld(), context.Background()
	w.labs[1] = []rewards.LabPass{{Lab: "a", XP: 100}}
	w.labs[2] = []rewards.LabPass{{Lab: "b", XP: 100}}
	if n, err := w.svc.EvaluateAll(ctx); err != nil || n != 2 {
		t.Fatalf("EvaluateAll = %d, %v", n, err)
	}
	// A badge added by a later import, for what was done before it
	w.catalog = append(w.catalog, badge("b-done", `{"type":"lab_passed","lab":"b"}`, 4))
	if n, err := w.svc.EvaluateAll(ctx); err != nil || n != 1 {
		t.Fatalf("new badge: %d, %v", n, err)
	}
	if _, ok := w.repo[2]["b-done"]; !ok {
		t.Fatalf("awards = %v", w.repo)
	}
}

func TestSummaryShowsLiveBadgesAndRetiredAwards(t *testing.T) {
	w, ctx := newWorld(), context.Background()
	w.labs[1] = []rewards.LabPass{{Lab: "a", XP: 90}, {Lab: "a", XP: 100}}
	w.quizzes[1] = []rewards.QuizPass{{Course: "k8s-intro", Item: "q", XP: 50}}
	w.repo[1] = map[string]time.Time{"old": t0}
	if _, err := w.svc.Evaluate(ctx, 1); err != nil {
		t.Fatal(err)
	}
	s, err := w.svc.Summary(ctx, 1)
	if err != nil || s.XP != 150 {
		t.Fatalf("summary = %+v, %v", s, err)
	}
	if got := ids(s.Badges); len(got) != 4 || got[0] != "first" || got[3] != "old" {
		t.Fatalf("badges = %v", got)
	}
	if s.Badges[0].AwardedAt == nil || s.Badges[1].AwardedAt != nil || s.Badges[3].AwardedAt == nil {
		t.Fatalf("awards = %+v", s.Badges)
	}
	if s2, _ := w.svc.Summary(ctx, 2); len(s2.Badges) != 3 || s2.XP != 0 {
		t.Fatalf("retired badge shown to a learner who never earned it: %v", ids(s2.Badges))
	}
}

func TestABrokenRuleDoesNotStopTheOthers(t *testing.T) {
	w, ctx := newWorld(), context.Background()
	w.catalog = append(memCatalog{badge("future", `{"type":"streak_days","min":3}`, -1)}, w.catalog...)
	w.labs[1] = []rewards.LabPass{{Lab: "a"}}
	if got, err := w.svc.Evaluate(ctx, 1); err != nil || len(got) != 1 || got[0].ID != "first" {
		t.Fatalf("got %v, %v", ids(got), err)
	}
}
