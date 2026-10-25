package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	catalogsqlite "github.com/IhebBelhadj/opsforge/academy/internal/catalog/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
)

type clock struct{ now time.Time }

func (c clock) Now() time.Time { return c.now }

var t0 = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func repo(t *testing.T) *catalogsqlite.Repo {
	t.Helper()
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "academy.db"), clock{t0})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return catalogsqlite.NewRepo(store.DB)
}

func TestEntriesRoundTrip(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	lab := catalog.Entry{Kind: catalog.KindLab, Key: "a", PackID: "p", Title: "A", Hash: "h1",
		Doc: json.RawMessage(`{"id":"a"}`)}
	item := catalog.Entry{Kind: catalog.KindItem, Key: "c/a", PackID: "p", CourseID: "c", Position: 2, Title: "A",
		Hash: "h1", Doc: json.RawMessage(`{"id":"a","section":0}`)}
	if err := r.Apply(ctx, []catalog.Entry{lab, item}, nil, t0); err != nil {
		t.Fatal(err)
	}
	got, err := r.Entries(ctx)
	if err != nil || len(got) != 2 || !got[0].Same(item) || !got[1].Same(lab) || got[0].Retired {
		t.Fatalf("got %+v, %v", got, err)
	}

	if err := r.Apply(ctx, nil, []catalog.Entry{lab}, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Entries(ctx); !got[1].Retired || got[0].Retired {
		t.Fatalf("retire: %+v", got)
	}
	if at, err := r.UpdatedAt(ctx, catalog.KindItem, "c/a"); err != nil || !at.Equal(t0) {
		t.Fatalf("untouched entry's time: %v, %v", at, err)
	}

	lab.Hash = "h2"
	if err := r.Apply(ctx, []catalog.Entry{lab}, nil, t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.Entries(ctx); got[1].Retired || got[1].Hash != "h2" {
		t.Fatalf("back: %+v", got[1])
	}
}

func TestApplyIsAllOrNothing(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	good := catalog.Entry{Kind: catalog.KindLab, Key: "a", PackID: "p", Title: "A", Hash: "h", Doc: json.RawMessage(`{}`)}
	bad := good
	bad.Kind = "nonsense" // the table's CHECK refuses it
	if err := r.Apply(ctx, []catalog.Entry{good, bad}, nil, t0); err == nil {
		t.Fatal("applied an entry of an unknown kind")
	}
	if got, _ := r.Entries(ctx); len(got) != 0 {
		t.Fatalf("half an import was kept: %+v", got)
	}
}

func TestLiveLeavesRetiredOut(t *testing.T) {
	r := repo(t)
	ctx := context.Background()
	course := func(pack, key string, pos int) catalog.Entry {
		return catalog.Entry{Kind: catalog.KindCourse, Key: key, PackID: pack, Position: pos, Title: key, Hash: "h",
			Doc: json.RawMessage(`{}`)}
	}
	gone := course("a", "gone", 0)
	entries := []catalog.Entry{course("b", "b1", 0), course("a", "a2", 2), gone, course("a", "a1", 1)}
	if err := r.Apply(ctx, entries, nil, t0); err != nil {
		t.Fatal(err)
	}
	if err := r.Apply(ctx, nil, []catalog.Entry{gone}, t0); err != nil {
		t.Fatal(err)
	}
	list, err := r.LiveList(ctx, catalog.KindCourse)
	var keys []string
	for _, e := range list {
		keys = append(keys, e.Key)
	}
	if err != nil || strings.Join(keys, " ") != "a1 a2 b1" {
		t.Fatalf("%v, %v", keys, err)
	}
	if e, err := r.Live(ctx, catalog.KindCourse, "a2"); err != nil || !e.Same(entries[1]) {
		t.Fatalf("%+v, %v", e, err)
	}
	for _, key := range []string{"gone", "nope"} {
		if _, err := r.Live(ctx, catalog.KindCourse, key); !errors.Is(err, catalog.ErrNotFound) {
			t.Fatalf("%s: %v", key, err)
		}
	}
	if list, _ := r.LiveList(ctx, catalog.KindLab); len(list) != 0 {
		t.Fatalf("labs %+v", list)
	}
}
