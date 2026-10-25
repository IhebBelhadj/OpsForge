package sqlite_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	identitysqlite "github.com/IhebBelhadj/opsforge/academy/internal/identity/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/labsession"
	labsqlite "github.com/IhebBelhadj/opsforge/academy/internal/labsession/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/legacy"
	legacysqlite "github.com/IhebBelhadj/opsforge/academy/internal/legacy/sqlite"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform"
	"github.com/IhebBelhadj/opsforge/academy/internal/platform/sqlite"
	rewardssqlite "github.com/IhebBelhadj/opsforge/academy/internal/rewards/sqlite"
)

// phase10 writes testdata/labs.phase10.sql into a labs.db and returns its path.
func phase10(t *testing.T, extra ...string) string {
	t.Helper()
	script, err := os.ReadFile("../../../testdata/labs.phase10.sql")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "labs.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, s := range append([]string{string(script)}, extra...) {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestMigrateOnceThenWhatIsNew(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	u, err := identitysqlite.NewRepo(store.DB).EnsureUser(ctx, "learner", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	svc := legacy.NewService(legacysqlite.Reader{}, legacysqlite.NewStore(store.DB))
	path := phase10(t)
	r, err := svc.Migrate(ctx, path, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r != (legacy.Report{Attempts: 2, Steps: 3, Hints: 1, Events: 7, Awards: 1}) {
		t.Fatalf("report %+v", r)
	}

	labs := labsqlite.NewRepo(store.DB)
	passed, err := labs.Passed(ctx, u.ID)
	if err != nil || len(passed) != 1 {
		t.Fatalf("passed %+v %v", passed, err)
	}
	a := passed[0]
	if a.Lab != "regions-and-latency" || a.Hash != "e406eb6eb5d93569" || a.XP != 90 || a.Seconds != 70 ||
		a.HintsUsed != 1 || a.Status != labsession.Passed || !a.StartedAt.Equal(time.Date(2026, 10, 24, 12, 49, 14, 0, time.UTC)) {
		t.Fatalf("attempt %+v", a)
	}
	if hints, _ := labs.Hints(ctx, a.ID); hints["ping"] != 1 {
		t.Fatalf("hints %v", hints)
	}
	if steps, _ := labs.PassedSteps(ctx, a.ID); !steps["ping"] {
		t.Fatalf("steps %v", steps)
	}
	if awards, _ := rewardssqlite.NewRepo(store.DB).Awards(ctx, u.ID); len(awards) != 1 {
		t.Fatalf("awards %v", awards)
	}
	var data string
	if err := store.DB.QueryRow(`SELECT data FROM events WHERE kind = 'lab.passed'`).Scan(&data); err != nil ||
		data != `{"attempt":`+strconv.FormatInt(a.ID, 10)+`,"hints":1,"seconds":70.0,"xp":90}` {
		t.Fatalf("lab.passed data %s %v", data, err)
	}

	// Again: nothing new
	if r, err := svc.Migrate(ctx, path, u.ID); err != nil || r != (legacy.Report{}) {
		t.Fatalf("second run %+v %v", r, err)
	}
	// Phase 10 was used since: only its new rows
	path = phase10(t, `INSERT INTO attempts (id, user_id, lab_id, content_hash, started_at, status) VALUES
		(3, 1, 'private-networks', '72aa9f03d131214c', '2026-10-24T13:00:00Z', 'running');
		INSERT INTO events (id, user_id, kind, subject, data, at) VALUES
		(8, 1, 'lab.started', 'private-networks', '{"attempt": 3}', '2026-10-24T13:00:00Z');`)
	if r, err := svc.Migrate(ctx, path, u.ID); err != nil || r != (legacy.Report{Attempts: 1, Events: 1}) {
		t.Fatalf("third run %+v %v", r, err)
	}
	if cur, ok, err := labs.Active(ctx, u.ID); err != nil || !ok || cur.Lab != "private-networks" {
		t.Fatalf("the running Phase 10 attempt: %+v %v %v", cur, ok, err)
	}
}

func TestAFailedRunWritesNothing(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "academy.db"), platform.SystemClock{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	u, _ := identitysqlite.NewRepo(store.DB).EnsureUser(ctx, "learner", time.Now())
	labs := labsqlite.NewRepo(store.DB)
	if _, err := labs.Create(ctx, labsession.Attempt{UserID: u.ID, Lab: "x", Hash: "h", Status: labsession.Running,
		StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	path := phase10(t, `INSERT INTO attempts (id, user_id, lab_id, content_hash, started_at, status) VALUES
		(3, 1, 'private-networks', 'h', '2026-10-24T13:00:00Z', 'running');`)
	svc := legacy.NewService(legacysqlite.Reader{}, legacysqlite.NewStore(store.DB))
	if _, err := svc.Migrate(ctx, path, u.ID); err == nil {
		t.Fatal("two running labs: no error")
	}
	var n int
	if err := store.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM attempts) + (SELECT COUNT(*) FROM events) +
		(SELECT COUNT(*) FROM legacy_rows)`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows left: %d %v", n, err)
	}
	if _, err := svc.Migrate(ctx, filepath.Join(t.TempDir(), "nope.db"), u.ID); err == nil {
		t.Fatal("a missing database: no error")
	}
}
