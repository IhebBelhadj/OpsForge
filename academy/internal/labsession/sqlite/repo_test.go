package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/labsession"
	labsqlite "github.com/IhebBelhadj/opsforge/academy/internal/labsession/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func repo(t *testing.T) (*labsqlite.Repo, int64, int64) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	users := identitysqlite.NewRepo(store.DB)
	a, _ := users.EnsureUser(ctx, "a", t0)
	b, _ := users.EnsureUser(ctx, "b", t0)
	return labsqlite.NewRepo(store.DB), a.ID, b.ID
}

func TestOneActiveAttemptPerUser(t *testing.T) {
	r, a, b := repo(t)
	ctx := context.Background()
	id, err := r.Create(ctx, labsession.Attempt{UserID: a, Lab: "meet", Hash: "h", Status: labsession.Setup, StartedAt: t0})
	if err != nil || id != 1 {
		t.Fatalf("%d %v", id, err)
	}
	if _, err := r.Create(ctx, labsession.Attempt{UserID: a, Lab: "x", Status: labsession.Setup, StartedAt: t0}); !errors.Is(err, labsession.ErrAnotherLabRunning) {
		t.Fatalf("second active: %v", err)
	}
	if _, err := r.Create(ctx, labsession.Attempt{UserID: b, Lab: "x", Status: labsession.Setup, StartedAt: t0}); err != nil {
		t.Fatalf("another user's: %v", err)
	}
	if n, err := r.Recover(ctx); err != nil || n != 2 {
		t.Fatalf("recover %d %v", n, err)
	}
	got, ok, err := r.Active(ctx, a)
	if err != nil || !ok || got.Status != labsession.Running || got.Course != "" || got.Namespace != "" {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	end := t0.Add(time.Minute)
	got.Status, got.FinishedAt, got.Error = labsession.Abandoned, &end, ""
	if err := r.Finish(ctx, got); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := r.Active(ctx, a); ok {
		t.Fatal("still active after finishing")
	}
	if _, err := r.Create(ctx, labsession.Attempt{UserID: a, Lab: "x", Status: labsession.Setup, StartedAt: t0}); err != nil {
		t.Fatalf("after the first ended: %v", err)
	}
}

func TestAttemptRoundTripStepsAndHints(t *testing.T) {
	r, a, _ := repo(t)
	ctx := context.Background()
	id, _ := r.Create(ctx, labsession.Attempt{UserID: a, Lab: "meet", Course: "k8s", Item: "meet", Hash: "h",
		Status: labsession.Running, StartedAt: t0})
	if err := r.SetPlaces(ctx, id, "lab-1", "lab-1"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		step   string
		passed bool
	}{{"one", false}, {"one", true}, {"two", false}} {
		if err := r.RecordStep(ctx, id, s.step, s.passed, json.RawMessage(`{}`), t0); err != nil {
			t.Fatal(err)
		}
	}
	if steps, _ := r.PassedSteps(ctx, id); len(steps) != 1 || !steps["one"] {
		t.Fatalf("%v", steps)
	}
	for _, idx := range []int{0, 0, 1} { // the same hint twice counts once
		if err := r.ShowHint(ctx, id, "one", idx, t0); err != nil {
			t.Fatal(err)
		}
	}
	if hints, _ := r.Hints(ctx, id); hints["one"] != 2 {
		t.Fatalf("%v", hints)
	}
	if err := r.ClearSteps(ctx, id); err != nil {
		t.Fatal(err)
	}
	if steps, _ := r.PassedSteps(ctx, id); len(steps) != 0 {
		t.Fatalf("after a reset: %v", steps)
	}
	got, _ := r.Attempt(ctx, id)
	end := t0.Add(95 * time.Second)
	got.Status, got.FinishedAt, got.Seconds, got.XP = labsession.Passed, &end, 95, 80
	if err := r.Finish(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, err := r.Attempt(ctx, id)
	if err != nil || got.HintsUsed != 2 || got.Namespace != "lab-1" || got.Workspace != "lab-1" || got.Course != "k8s" ||
		got.Status != labsession.Passed || got.XP != 80 || got.Seconds != 95 || !got.FinishedAt.Equal(end) {
		t.Fatalf("%+v %v", got, err)
	}
	passed, err := r.Passed(ctx, a)
	if err != nil || len(passed) != 1 || passed[0].ID != id {
		t.Fatalf("%+v %v", passed, err)
	}
	if _, err := r.Attempt(ctx, 99); !errors.Is(err, labsession.ErrAttemptNotFound) {
		t.Fatalf("%v", err)
	}
}
