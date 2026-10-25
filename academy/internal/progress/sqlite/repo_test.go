package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/progress"
	progresssqlite "github.com/IhebBelhadj/opsforge/academy/internal/progress/sqlite"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func repo(t *testing.T) (*progresssqlite.Repo, int64) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	u, err := identitysqlite.NewRepo(store.DB).EnsureUser(ctx, "learner", t0)
	if err != nil {
		t.Fatal(err)
	}
	return progresssqlite.NewRepo(store.DB), u.ID
}

func TestRecords(t *testing.T) {
	r, user := repo(t)
	ctx := context.Background()
	if err := r.Open(ctx, user, "c", "lec", t0); err != nil {
		t.Fatal(err)
	}
	if err := r.Complete(ctx, user, "c", "lec", "h1", t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := r.Open(ctx, user, "c", "quiz", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := r.Open(ctx, user, "other", "x", t0); err != nil {
		t.Fatal(err)
	}
	got, err := r.Records(ctx, user, "c")
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	lec := got["lec"]
	if lec.Hash != "h1" || !lec.CompletedAt.Equal(t0.Add(time.Minute)) || !lec.OpenedAt.Equal(t0) {
		t.Fatalf("lecture %+v", lec)
	}
	if q := got["quiz"]; q.CompletedAt != nil || q.Hash != "" || !q.OpenedAt.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("quiz %+v", q)
	}
	if err := r.Complete(ctx, user, "c", "lec", "h2", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Records(ctx, user, "c"); got["lec"].Hash != "h2" || !got["lec"].OpenedAt.Equal(t0) {
		t.Fatalf("completing again: %+v", got["lec"])
	}
}

func TestCourseCompletionsKeepTheFirst(t *testing.T) {
	r, user := repo(t)
	ctx := context.Background()
	for i, want := range []bool{true, false} {
		first, err := r.CompleteCourse(ctx, user, "c", t0.Add(time.Duration(i)*time.Hour))
		if err != nil || first != want {
			t.Fatalf("completion %d: %v %v", i, first, err)
		}
	}
	got, err := r.CourseCompletions(ctx, user)
	if err != nil || len(got) != 1 || !got["c"].Equal(t0) {
		t.Fatalf("%v %v", got, err)
	}
}

func TestNotes(t *testing.T) {
	r, user := repo(t)
	ctx := context.Background()
	if n, err := r.Note(ctx, user, "c", "i"); err != nil || n.UpdatedAt != nil {
		t.Fatalf("%+v %v", n, err)
	}
	for _, body := range []string{"first", "second"} {
		at := t0
		if err := r.SaveNote(ctx, user, "c", "i", progress.Note{Body: body, UpdatedAt: &at}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := r.Note(ctx, user, "c", "i"); err != nil || n.Body != "second" || !n.UpdatedAt.Equal(t0) {
		t.Fatalf("%+v %v", n, err)
	}
}
