package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	historysqlite "github.com/IhebBelhadj/opsforge/academy/internal/history/sqlite"
	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
)

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func TestAppendAndAfter(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	users := identitysqlite.NewRepo(store.DB)
	a, _ := users.EnsureUser(ctx, "a", t0)
	b, _ := users.EnsureUser(ctx, "b", t0)
	r := historysqlite.NewRepo(store.DB)
	for i, u := range []int64{a.ID, b.ID, a.ID} {
		id, err := r.Append(ctx, platform.Event{UserID: u, Kind: "lab.passed", Subject: "meet",
			Data: json.RawMessage(`{"xp":90}`), At: t0.Add(time.Duration(i) * time.Second)})
		if err != nil || id != int64(i+1) {
			t.Fatalf("append %d: id %d, %v", i, id, err)
		}
	}
	got, err := r.After(ctx, a.ID, 0, 10)
	if err != nil || len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("%+v %v", got, err)
	}
	if e := got[1]; e.Kind != "lab.passed" || e.Subject != "meet" || string(e.Data) != `{"xp":90}` ||
		!e.At.Equal(t0.Add(2*time.Second)) || e.UserID != a.ID {
		t.Fatalf("round trip %+v", e)
	}
	if got, _ := r.After(ctx, a.ID, 1, 10); len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("after 1: %+v", got)
	}
	if got, _ := r.After(ctx, a.ID, 0, 1); len(got) != 1 {
		t.Fatalf("limit: %+v", got)
	}
	if _, err := r.Append(ctx, platform.Event{UserID: 99, Kind: "x", Data: json.RawMessage(`{}`), At: t0}); err == nil {
		t.Fatal("an event of an unknown user was stored")
	}
}
