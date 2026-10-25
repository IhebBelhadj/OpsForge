package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
	rewardssqlite "github.com/IhebBelhadj/opsforge/academy/internal/rewards/sqlite"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func TestAwardsOnce(t *testing.T) {
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
	r := rewardssqlite.NewRepo(store.DB)
	if first, err := r.Award(ctx, u.ID, "first", t0); err != nil || !first {
		t.Fatalf("award: %v %v", first, err)
	}
	if first, err := r.Award(ctx, u.ID, "first", t0.Add(time.Hour)); err != nil || first {
		t.Fatalf("second award: %v %v", first, err)
	}
	got, err := r.Awards(ctx, u.ID)
	if err != nil || len(got) != 1 || !got["first"].Equal(t0) {
		t.Fatalf("awards = %v, %v", got, err)
	}
	if got, err := r.Awards(ctx, u.ID+1); err != nil || len(got) != 0 {
		t.Fatalf("another user's awards = %v, %v", got, err)
	}
}
