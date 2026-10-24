package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
)

type fixedClock time.Time

func (c fixedClock) Now() time.Time { return time.Time(c) }

var t0 = fixedClock(time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC))

func open(t *testing.T, path string, clock fixedClock) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(context.Background(), path, clock)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return store
}

func TestOpenAppliesMigrationsAndRecordsTheInstanceOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sub", "academy.db")
	store := open(t, path, t0)
	created, err := store.Queries.GetInstance(ctx)
	if err != nil || created != "2026-10-24T12:00:00Z" {
		t.Fatalf("instance created_at = %q, %v", created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again := open(t, path, fixedClock(time.Time(t0).Add(time.Hour)))
	defer func() { _ = again.Close() }()
	if created, _ := again.Queries.GetInstance(ctx); created != "2026-10-24T12:00:00Z" {
		t.Fatalf("reopening rewrote created_at: %q", created)
	}
	if err := again.Ready(ctx); err != nil {
		t.Fatalf("Ready: %v", err)
	}
}

func TestOpenSetsThePragmas(t *testing.T) {
	store := open(t, filepath.Join(t.TempDir(), "academy.db"), t0)
	defer func() { _ = store.Close() }()
	for pragma, want := range map[string]string{"foreign_keys": "1", "journal_mode": "wal", "busy_timeout": "5000"} {
		var got string
		if err := store.DB.QueryRow("PRAGMA " + pragma).Scan(&got); err != nil || got != want {
			t.Errorf("PRAGMA %s = %q (%v), want %q", pragma, got, err, want)
		}
	}
}

func TestReadyFailsOnAClosedStore(t *testing.T) {
	store := open(t, filepath.Join(t.TempDir(), "academy.db"), t0)
	_ = store.Close()
	if err := store.Ready(context.Background()); err == nil {
		t.Fatal("Ready on a closed store: want an error")
	}
}

func TestOpenFailsOnAnUnwritablePath(t *testing.T) {
	if _, err := sqlite.Open(context.Background(), "/proc/academy/academy.db", t0); err == nil {
		t.Fatal("want an error")
	}
}

func TestStoredTimesSortAsText(t *testing.T) {
	a := time.Date(2026, 10, 24, 9, 0, 0, 5, time.UTC)
	b := a.Add(time.Nanosecond * 999_999_990) // more digits after the second, still later
	c := time.Date(2026, 10, 24, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	if sqlite.FormatTime(a) >= sqlite.FormatTime(b) || sqlite.FormatTime(b) <= sqlite.FormatTime(c) {
		t.Fatalf("%s %s %s", sqlite.FormatTime(a), sqlite.FormatTime(b), sqlite.FormatTime(c))
	}
	if got, err := sqlite.ParseTime(sqlite.FormatTime(c)); err != nil || !got.Equal(c) {
		t.Fatalf("round trip: %v, %v", got, err)
	}
}
