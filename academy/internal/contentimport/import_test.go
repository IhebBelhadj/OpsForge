package contentimport_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/IhebBelhadj/opsforge/academy/internal/catalog"
	catalogsqlite "github.com/IhebBelhadj/opsforge/academy/internal/catalog/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/contentimport"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
)

// The Phase 10 acceptance on the new stack: the golden bundle imported twice into a real database changes nothing.
func TestImportTwiceIntoTheDatabase(t *testing.T) {
	c := &clock{now: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)}
	store, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "academy.db"), c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	repo := catalogsqlite.NewRepo(store.DB)
	svc := contentimport.NewService(&compiler{bundle: golden(t)}, repo, c)
	ctx := context.Background()

	if r, err := svc.Import(ctx); err != nil || !r.Changed {
		t.Fatalf("first: %+v, %v", r, err)
	}
	c.now = c.now.Add(time.Hour)
	r, err := svc.Import(ctx)
	if err != nil || r.Changed || r.Unchanged[catalog.KindPack] != 1 || r.Unchanged[catalog.KindItem] != 4 {
		t.Fatalf("second: %+v, %v", r, err)
	}
	if at, err := repo.UpdatedAt(ctx, catalog.KindPack, "demo"); err != nil || !at.Equal(c.now.Add(-time.Hour)) {
		t.Fatalf("pack imported at %v, %v", at, err)
	}
}
