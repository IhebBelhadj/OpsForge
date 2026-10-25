package progress_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/progress"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

type key struct {
	user         int64
	course, item string
}

type memRepo struct {
	mu      sync.Mutex
	records map[key]progress.Record
	courses map[int64]map[string]time.Time
	notes   map[key]progress.Note
}

func newRepo() *memRepo {
	return &memRepo{records: map[key]progress.Record{}, courses: map[int64]map[string]time.Time{},
		notes: map[key]progress.Note{}}
}

func (r *memRepo) Records(_ context.Context, user int64, course string) (map[string]progress.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]progress.Record{}
	for k, v := range r.records {
		if k.user == user && k.course == course {
			out[k.item] = v
		}
	}
	return out, nil
}

func (r *memRepo) Complete(_ context.Context, user int64, course, item, hash string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.records[key{user, course, item}]
	rec.Hash, rec.CompletedAt = hash, &at
	r.records[key{user, course, item}] = rec
	return nil
}

func (r *memRepo) Open(_ context.Context, user int64, course, item string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec := r.records[key{user, course, item}]
	rec.OpenedAt = &at
	r.records[key{user, course, item}] = rec
	return nil
}

func (r *memRepo) CompleteCourse(_ context.Context, user int64, course string, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.courses[user] == nil {
		r.courses[user] = map[string]time.Time{}
	}
	if _, ok := r.courses[user][course]; ok {
		return false, nil
	}
	r.courses[user][course] = at
	return true, nil
}

func (r *memRepo) CourseCompletions(_ context.Context, user int64) (map[string]time.Time, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]time.Time{}
	for c, t := range r.courses[user] {
		out[c] = t
	}
	return out, nil
}

func (r *memRepo) Note(_ context.Context, user int64, course, item string) (progress.Note, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.notes[key{user, course, item}], nil
}

func (r *memRepo) SaveNote(_ context.Context, user int64, course, item string, n progress.Note) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notes[key{user, course, item}] = n
	return nil
}

type courses struct{ list []catalog.Course }

func (c courses) Courses(context.Context) ([]catalog.CourseSummary, error) {
	out := make([]catalog.CourseSummary, len(c.list))
	for i, co := range c.list {
		out[i] = co.CourseSummary
	}
	return out, nil
}

func (c courses) Course(_ context.Context, id string) (catalog.Course, error) {
	for _, co := range c.list {
		if co.ID == id {
			return co, nil
		}
	}
	return catalog.Course{}, catalog.ErrNotFound
}

// bus records what is published and hands it to the handler, like history.Service.
type bus struct {
	events  []platform.Event
	handler func(context.Context, platform.Event) error
}

func (b *bus) Publish(ctx context.Context, e platform.Event) (platform.Event, error) {
	e.ID = int64(len(b.events) + 1)
	b.events = append(b.events, e)
	if b.handler != nil {
		if err := b.handler(ctx, e); err != nil {
			return e, err
		}
	}
	return e, nil
}

func (b *bus) kinds() string {
	var ks []string
	for _, e := range b.events {
		ks = append(ks, e.Kind+":"+e.Subject)
	}
	return strings.Join(ks, " ")
}

type fixture struct {
	svc   *progress.Service
	repo  *memRepo
	bus   *bus
	clock *clock
	done  *[]progress.Completion
}

func newFixture() fixture {
	repo, b, c := newRepo(), &bus{}, &clock{now: t0}
	done := &[]progress.Completion{}
	source := progress.SourceFunc(func(context.Context, int64) ([]progress.Completion, error) { return *done, nil })
	small := catalog.Course{CourseSummary: catalog.CourseSummary{ID: "small"}, Sections: []catalog.Section{
		{Items: []catalog.ItemRef{{ID: "meet", Type: "lab", Ref: "meet-the-cluster", Hash: "B1"}}},
	}}
	svc := progress.NewService(repo, courses{[]catalog.Course{course(), small}}, b, c, progress.Sources{source})
	b.handler = svc.OnEvent
	return fixture{svc: svc, repo: repo, bus: b, clock: c, done: done}
}

func TestCompletingALecture(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	ip, err := f.svc.CompleteLecture(ctx, 1, "k8s-intro", "what-is-a-pod")
	if err != nil || ip.Status != progress.Completed || !ip.CompletedAt.Equal(t0) {
		t.Fatalf("%+v %v", ip, err)
	}
	if f.bus.kinds() != "lecture.completed:k8s-intro/what-is-a-pod" {
		t.Fatalf("events %s", f.bus.kinds())
	}
	f.clock.now = t0.Add(time.Hour)
	again, err := f.svc.CompleteLecture(ctx, 1, "k8s-intro", "what-is-a-pod")
	if err != nil || !again.CompletedAt.Equal(t0) || len(f.bus.events) != 1 {
		t.Fatalf("completing again changed it: %+v, %d events, %v", again, len(f.bus.events), err)
	}

	// The lecture is updated: it shows stale until completed again
	f.repo.records[key{1, "k8s-intro", "what-is-a-pod"}] = progress.Record{Hash: "L1", CompletedAt: at(0)}
	p, _ := f.svc.Course(ctx, 1, "k8s-intro")
	if !p.Items[0].Stale {
		t.Fatalf("not stale: %+v", p.Items[0])
	}
	if _, err := f.svc.CompleteLecture(ctx, 1, "k8s-intro", "what-is-a-pod"); err != nil {
		t.Fatal(err)
	}
	if p, _ := f.svc.Course(ctx, 1, "k8s-intro"); p.Items[0].Stale || len(f.bus.events) != 2 {
		t.Fatalf("still stale after completing again: %+v (%d events)", p.Items[0], len(f.bus.events))
	}
}

func TestOnlyLecturesOfLiveCoursesAreCompletedByHand(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	for _, tc := range []struct {
		course, item string
		want         error
	}{
		{"k8s-intro", "pods-check", progress.ErrNotALecture},
		{"k8s-intro", "nope", catalog.ErrNotFound},
		{"nope", "what-is-a-pod", catalog.ErrNotFound},
	} {
		if _, err := f.svc.CompleteLecture(ctx, 1, tc.course, tc.item); !errors.Is(err, tc.want) {
			t.Errorf("%s/%s: err = %v, want %v", tc.course, tc.item, err, tc.want)
		}
	}
	if len(f.bus.events) != 0 {
		t.Fatalf("events %s", f.bus.kinds())
	}
}

func TestOpeningSetsTheResumePoint(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	if err := f.svc.Open(ctx, 1, "k8s-intro", "meet"); err != nil {
		t.Fatal(err)
	}
	p, err := f.svc.Course(ctx, 1, "k8s-intro")
	if err != nil || p.Resume != "meet" || p.Items[2].Status != progress.InProgress {
		t.Fatalf("%+v %v", p, err)
	}
	if err := f.svc.Open(ctx, 1, "k8s-intro", "nope"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown item: %v", err)
	}
	if other, _ := f.svc.Course(ctx, 2, "k8s-intro"); other.Resume != "what-is-a-pod" {
		t.Fatalf("another user's progress leaked: %+v", other)
	}
}

func TestACourseCompletedIsRecordedAndAnnouncedOnce(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	// The lab is passed (its module publishes lab.passed): it completes "small", not k8s-intro
	*f.done = append(*f.done, progress.Completion{Type: "lab", Ref: "meet-the-cluster", Hash: "B1", At: t0})
	if _, err := f.bus.Publish(ctx, platform.Event{UserID: 1, Kind: "lab.passed", Subject: "meet-the-cluster"}); err != nil {
		t.Fatal(err)
	}
	if f.bus.kinds() != "lab.passed:meet-the-cluster course.completed:small" {
		t.Fatalf("events %s", f.bus.kinds())
	}
	if _, err := f.bus.Publish(ctx, platform.Event{UserID: 1, Kind: "lab.passed", Subject: "meet-the-cluster"}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(f.bus.kinds(), "course.completed") != 1 {
		t.Fatalf("announced twice: %s", f.bus.kinds())
	}
	p, _ := f.svc.Course(ctx, 1, "small")
	if !p.Complete || p.CompletedAt == nil || p.Resume != "" {
		t.Fatalf("%+v", p)
	}
	// Events that complete nothing are ignored
	n := len(f.bus.events)
	_ = f.svc.OnEvent(ctx, platform.Event{UserID: 1, Kind: "hint.shown"})
	if len(f.bus.events) != n {
		t.Fatal("a hint completed a course")
	}
}

func TestOverviewListsEveryCourse(t *testing.T) {
	f := newFixture()
	list, err := f.svc.Overview(context.Background(), 1)
	if err != nil || len(list) != 2 || list[0].Course != "k8s-intro" || list[1].Total != 1 {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestNotes(t *testing.T) {
	f := newFixture()
	ctx := context.Background()
	n, err := f.svc.Note(ctx, 1, "k8s-intro", "meet")
	if err != nil || n.Body != "" || n.UpdatedAt != nil {
		t.Fatalf("no note yet: %+v %v", n, err)
	}
	if _, err := f.svc.SaveNote(ctx, 1, "k8s-intro", "meet", "remember -o wide"); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.svc.Note(ctx, 1, "k8s-intro", "meet"); n.Body != "remember -o wide" || !n.UpdatedAt.Equal(t0) {
		t.Fatalf("%+v", n)
	}
	if _, err := f.svc.SaveNote(ctx, 1, "k8s-intro", "meet", strings.Repeat("x", progress.MaxNote+1)); !errors.Is(err, progress.ErrNoteTooLong) {
		t.Fatalf("long note: %v", err)
	}
	if _, err := f.svc.SaveNote(ctx, 1, "k8s-intro", "nope", "x"); !errors.Is(err, catalog.ErrNotFound) {
		t.Fatalf("unknown item: %v", err)
	}
}
