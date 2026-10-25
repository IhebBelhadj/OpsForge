package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/workspace"
	workspacesqlite "github.com/IhebBelhadj/opsforge/academy/internal/workspace/sqlite"
)

func TestOwners(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	u, _ := identitysqlite.NewRepo(store.DB).EnsureUser(ctx, "learner", time.Now())
	r := workspacesqlite.NewRepo(store.DB)
	if err := r.Create(ctx, "lab-1", u.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := r.Create(ctx, "lab-1", u.ID, time.Now()); err == nil {
		t.Fatal("created twice")
	}
	if owner, err := r.Owner(ctx, "lab-1"); err != nil || owner != u.ID {
		t.Fatalf("%d %v", owner, err)
	}
	if _, err := r.Owner(ctx, "lab-2"); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("%v", err)
	}
}
